package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/maypok86/otter/v2"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
)

const (
	AuthService                = "auth"
	defaultBackpressureTimeout = 10 * time.Millisecond
)

type UserLimiter interface {
	CheckUsers(ctx context.Context, users map[int32]uint) error
	// for properties we want to ensure they belong to an org owned by an active subscriber
	EvaluatePropertyAccess(ctx context.Context, userID int32) (bool, error)
	// for API we want to check if user is accessing a resource owned by an active subscriber
	// (but this check is more down the callstack inside Verifier)
	EvaluateAPIAccess(ctx context.Context, userID int32) (bool, error)
	// dropping a user means they will be checked again
	DropUser(ctx context.Context, userID int32)
}

type AuthMiddleware struct {
	Store                 db.Implementor
	PlanService           billing.PlanService
	SitekeyChan           chan string
	FormChan              chan string
	UsersChan             chan int32
	APIKeyLastUsedChan    chan int32
	RulesChan             chan int32
	BatchSize             int
	SitekeyBackfillCancel context.CancelFunc
	FormBackfillCancel    context.CancelFunc
	UsersBackfillCancel   context.CancelFunc
	APIKeyLastUsedCancel  context.CancelFunc
	RulesBackfillCancel   context.CancelFunc
	Limiter               UserLimiter
	backpressureTimeout   time.Duration
	Metrics               common.BaseMetrics
	RulesCompiler         rules.Compiler
	// this is a simple way to control negative cache spam, disabled by default
	NegativeSitekeyThreshold uint
}

type baseUserLimiter struct {
	store      db.Implementor
	userLimits common.Cache[int32, bool]
}

var _ UserLimiter = (*baseUserLimiter)(nil)

func (ul *baseUserLimiter) unknownUsers(ctx context.Context, users map[int32]uint) []int32 {
	result := make([]int32, 0, len(users))

	for userID := range users {
		if _, err := ul.userLimits.Get(ctx, userID); err == db.ErrCacheMiss {
			result = append(result, userID)
		}
	}

	return result
}

func (ul *baseUserLimiter) DropUser(ctx context.Context, userID int32) {
	if found := ul.userLimits.Delete(ctx, userID); found {
		slog.DebugContext(ctx, "Removed user from user limiter", "userID", userID)
	}
}

func (ul *baseUserLimiter) CheckUsers(ctx context.Context, batch map[int32]uint) error {
	if len(batch) == 0 {
		slog.DebugContext(ctx, "No users to check")
		return nil
	}

	unknownUsers := ul.unknownUsers(ctx, batch)
	if len(unknownUsers) == 0 {
		slog.DebugContext(ctx, "All user limits were recently checked", "count", len(batch))
		return nil
	}

	t := struct{}{}
	users, err := ul.store.Impl().RetrieveUsersWithoutSubscription(ctx, unknownUsers)
	if err == nil {
		violatorsMap := make(map[int32]struct{}, len(users))
		for _, u := range users {
			_ = ul.userLimits.Set(ctx, u.ID, true)
			violatorsMap[u.ID] = t
		}

		for _, u := range unknownUsers {
			if _, found := violatorsMap[u]; !found {
				_ = ul.userLimits.SetMissing(ctx, u)
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to check users without subscriptions", "count", len(unknownUsers), common.ErrAttr(err))
	}

	return err
}

func (ul *baseUserLimiter) EvaluateAPIAccess(ctx context.Context, userID int32) (bool, error) {
	_, err := ul.userLimits.Get(ctx, userID)
	// "false" because by we only check if user has a subscription at all, we don't verify usage limits
	return false, err
}

func (ul *baseUserLimiter) EvaluatePropertyAccess(ctx context.Context, userID int32) (bool, error) {
	return ul.EvaluateAPIAccess(ctx, userID)
}

func NewUserLimiter(store db.Implementor) *baseUserLimiter {
	const maxLimitedUsers = 10_000
	const userLimitTTL = 30 * time.Minute
	var userLimits common.Cache[int32, bool]
	var err error
	// missing TTL should be equal to "usual" TTL here because it has the same meaning (we mark user has no violation)
	userLimits, err = db.NewMemoryCacheEx[int32, bool]("user_limits", maxLimitedUsers, false /*missing value*/, userLimitTTL,
		func(o *otter.Options[int32, bool]) {
			// we want to ONLY use ExpiryAccessing so that we _force_ re-checking various user limit conditions
			o.ExpiryCalculator = otter.ExpiryAccessing[int32, bool](userLimitTTL)
		})
	if err != nil {
		slog.Error("Failed to create memory cache for user limits", common.ErrAttr(err))
		userLimits = db.NewStaticCache[int32, bool](maxLimitedUsers, false /*missing data*/)
	}

	return &baseUserLimiter{
		userLimits: userLimits,
		store:      store,
	}
}

func NewAuthMiddleware(store db.Implementor,
	userLimiter UserLimiter,
	planService billing.PlanService,
	metrics common.BaseMetrics,
	rulesCompiler rules.Compiler) *AuthMiddleware {
	const batchSize = 10
	const apiKeyLastUsedChannelSize = 250

	am := &AuthMiddleware{
		Store:                 store,
		Limiter:               userLimiter,
		PlanService:           planService,
		SitekeyChan:           make(chan string, 100*batchSize),
		FormChan:              make(chan string, 100*batchSize),
		UsersChan:             make(chan int32, 10*batchSize),
		APIKeyLastUsedChan:    make(chan int32, apiKeyLastUsedChannelSize),
		RulesChan:             make(chan int32, 10*batchSize),
		BatchSize:             batchSize,
		Metrics:               metrics,
		RulesCompiler:         rulesCompiler,
		SitekeyBackfillCancel: func() {},
		FormBackfillCancel:    func() {},
		UsersBackfillCancel:   func() {},
		APIKeyLastUsedCancel:  func() {},
		RulesBackfillCancel:   func() {},
		backpressureTimeout:   defaultBackpressureTimeout,
	}

	return am
}

func (am *AuthMiddleware) StartBackfill(backfillDelay, backpressureTimeout time.Duration) {
	am.backpressureTimeout = max(backpressureTimeout, defaultBackpressureTimeout)

	var sitekeyBackfillCtx context.Context
	sitekeyBackfillBaseCtx := context.WithValue(context.Background(), common.ServiceContextKey, AuthService)
	sitekeyBackfillCtx, am.SitekeyBackfillCancel = context.WithCancel(
		context.WithValue(sitekeyBackfillBaseCtx, common.TraceIDContextKey, "sitekey_backfill"))
	go common.ProcessBatchMap(sitekeyBackfillCtx, am.SitekeyChan, backfillDelay, am.BatchSize, am.BatchSize*100, am.backfillSitekeyImpl)

	var formBackfillCtx context.Context
	formBackfillBaseCtx := context.WithValue(context.Background(), common.ServiceContextKey, AuthService)
	formBackfillCtx, am.FormBackfillCancel = context.WithCancel(
		context.WithValue(formBackfillBaseCtx, common.TraceIDContextKey, "form_backfill"))
	go common.ProcessBatchMap(formBackfillCtx, am.FormChan, backfillDelay, am.BatchSize, am.BatchSize*100, am.backfillFormsImpl)

	var usersBackfillCtx context.Context
	userBackfillBaseCtx := context.WithValue(context.Background(), common.ServiceContextKey, AuthService)
	usersBackfillCtx, am.UsersBackfillCancel = context.WithCancel(
		context.WithValue(userBackfillBaseCtx, common.TraceIDContextKey, "users_backfill"))
	// NOTE: we use the same backfill delay because users processing is slower and sitekey channel will block on it
	go common.ProcessBatchMap(usersBackfillCtx, am.UsersChan, backfillDelay, am.BatchSize, am.BatchSize*10, am.backfillUsersImpl)

	// API key last used - use generous timeouts and batch sizes since we don't need to update too often
	const apiKeyLastUsedBatchSize = 100
	const apiKeyLastUsedMaxBatchSize = 1000
	var apiKeyLastUsedCtx context.Context
	apiKeyLastUsedBaseCtx := context.WithValue(context.Background(), common.ServiceContextKey, AuthService)
	apiKeyLastUsedCtx, am.APIKeyLastUsedCancel = context.WithCancel(
		context.WithValue(apiKeyLastUsedBaseCtx, common.TraceIDContextKey, "apikey_lastused"))
	// Use a more generous delay (5x the regular backfill delay) since we don't need frequent updates
	apiKeyLastUsedDelay := backfillDelay * 5
	go common.ProcessBatchMap(apiKeyLastUsedCtx, am.APIKeyLastUsedChan, apiKeyLastUsedDelay, apiKeyLastUsedBatchSize, apiKeyLastUsedMaxBatchSize, am.backfillAPIKeyLastUsedImpl)

	var rulesBackfillCtx context.Context
	rulesBackfillBaseCtx := context.WithValue(context.Background(), common.ServiceContextKey, AuthService)
	rulesBackfillCtx, am.RulesBackfillCancel = context.WithCancel(
		context.WithValue(rulesBackfillBaseCtx, common.TraceIDContextKey, "rules_backfill"))
	go common.ProcessBatchMap(rulesBackfillCtx, am.RulesChan, backfillDelay, am.BatchSize, am.BatchSize*10, am.backfillRulesImpl)
}

func (am *AuthMiddleware) Shutdown() {
	slog.Debug("Shutting down auth middleware")
	am.SitekeyBackfillCancel()
	am.FormBackfillCancel()
	am.UsersBackfillCancel()
	am.APIKeyLastUsedCancel()
	am.RulesBackfillCancel()
	close(am.SitekeyChan)
	close(am.FormChan)
	close(am.UsersChan)
	close(am.APIKeyLastUsedChan)
	close(am.RulesChan)
}

func (am *AuthMiddleware) backfillFormsImpl(ctx context.Context, batch map[string]uint) error {
	forms, err := am.Store.Impl().RetrieveFormsByExternalID(ctx, batch, am.NegativeSitekeyThreshold)
	if err != nil {
		level := slog.LevelError
		if err == db.ErrNegativeCacheHit {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "Failed to retrieve forms by external ID", "count", len(batch), common.ErrAttr(err))
		if (err == db.ErrInvalidInput) || (err == db.ErrNegativeCacheHit) {
			return nil
		}
		return err
	}

	properties := make(map[int32]uint, len(forms))
	for _, form := range forms {
		properties[form.PropertyID] = 1
	}

	if len(properties) == 0 {
		return nil
	}

	props, err := am.Store.Impl().RetrievePropertiesByID(ctx, properties)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve form properties", common.ErrAttr(err))
		return err
	}

	for _, p := range props {
		if p.OrgOwnerID.Valid {
			select {
			case am.UsersChan <- p.OrgOwnerID.Int32:
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(am.backpressureTimeout):
				am.Metrics.ObserveEventDropped(common.UserLimitEventType)
			}
		}
	}

	return nil
}

// we cache properties and send owners down the background pipeline
func (am *AuthMiddleware) backfillSitekeyImpl(ctx context.Context, batch map[string]uint) error {
	properties, err := am.Store.Impl().RetrievePropertiesBySitekey(ctx, batch, am.NegativeSitekeyThreshold)
	if err != nil {
		level := slog.LevelError
		if err == db.ErrNegativeCacheHit {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "Failed to retrieve properties by sitekey", "count", len(batch), common.ErrAttr(err))
		if (err == db.ErrInvalidInput) || (err == db.ErrNegativeCacheHit) {
			// this is to break the reprocessing cycle in batch.go
			return nil
		}
		return err
	}

	const maxOrgsToPull = 10
	orgs := make(map[int32]struct{}, len(properties))

	for _, p := range properties {
		if p.OrgOwnerID.Valid {
			select {
			case am.UsersChan <- p.OrgOwnerID.Int32:
			case <-ctx.Done():
				slog.WarnContext(ctx, "Context cancelled for sitekey backfill implementation", "part", "org owner")
				return ctx.Err()
			case <-time.After(am.backpressureTimeout):
				am.Metrics.ObserveEventDropped(common.UserLimitEventType)
			}
		}

		if p.CreatorID.Valid && (!p.OrgOwnerID.Valid || (p.CreatorID.Int32 != p.OrgOwnerID.Int32)) {
			select {
			case am.UsersChan <- p.CreatorID.Int32:
			case <-ctx.Done():
				slog.WarnContext(ctx, "Context cancelled for sitekey backfill implementation", "part", "property creator")
				return ctx.Err()
			case <-time.After(am.backpressureTimeout):
				am.Metrics.ObserveEventDropped(common.UserLimitEventType)
			}
		}

		am.RefreshPropertyRules(ctx, p.ID)

		// this is an opportunistic process anyways. Other users should be checked via API key mechanism or eventually here
		if len(orgs) < maxOrgsToPull {
			if orgMembers, err := am.Store.Impl().RetrieveOrganizationUsers(ctx, p.OrgID.Int32); err == nil {
				for _, user := range orgMembers {
					select {
					case am.UsersChan <- user.User.ID:
					case <-ctx.Done():
						slog.WarnContext(ctx, "Context cancelled for sitekey backfill implementation", "part", "org users")
						return ctx.Err()
					case <-time.After(am.backpressureTimeout):
						am.Metrics.ObserveEventDropped(common.UserLimitEventType)
					}
				}
			}
			orgs[p.OrgID.Int32] = struct{}{}
		}
	}

	return nil
}

// we block users without a subscription and (re)cache users API keys to ensure smooth auth in /verify codepath
func (am *AuthMiddleware) backfillUsersImpl(ctx context.Context, batch map[int32]uint) error {
	if err := am.Limiter.CheckUsers(ctx, batch); err != nil {
		slog.ErrorContext(ctx, "Failed to check user limits", common.ErrAttr(err))
		// NOTE: we ignore this error because it is not critical for retry
	}

	// TODO: Refactor linear fetching of API keys to use batch mode
	// we do it linearly instead of in a batch with the assumption that most of these will be cached
	// (to be verified in metrics)
	// but we can use another SQL query and also BulkGet API of otter (postponed as benefit is not obvious _atm_)
	// also the same is in WarmupAPICacheJob (maintenance)
	for userID := range batch {
		if _, err := am.Store.Impl().RetrieveUserAPIKeys(ctx, userID); err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve users API keys", "userID", userID, common.ErrAttr(err))
		}
	}

	// we ignore errors as both of the above are not critical to retry the batch
	return nil
}

// we update last_used_at for API keys that were used
func (am *AuthMiddleware) backfillAPIKeyLastUsedImpl(ctx context.Context, batch map[int32]uint) error {
	if len(batch) == 0 {
		return nil
	}

	apiKeyIDs := make([]int32, 0, len(batch))
	for apiKeyID := range batch {
		apiKeyIDs = append(apiKeyIDs, apiKeyID)
	}

	if err := am.Store.Impl().UpdateAPIKeysLastUsedAt(ctx, apiKeyIDs); err != nil {
		slog.ErrorContext(ctx, "Failed to update API keys last used at", common.ErrAttr(err))
		return err
	}

	return nil
}

func (am *AuthMiddleware) backfillRulesImpl(ctx context.Context, batch map[int32]uint) error {
	if len(batch) == 0 {
		return nil
	}

	impl := am.Store.Impl()

	// collect property IDs that don't have cached compiled property rules
	uncachedPropertyIDs := make(map[int32]uint, len(batch))

	for propertyID, count := range batch {
		// because we have 2-layered cache (raw rules -> compiled rules) so when we detect that we would have wanted to
		// reread our compiled rules ("refresh" in otter's terminology), we _actually_ want to recompile originals
		_, needsRefresh, err := impl.GetCachedCompiledPropertyRules(ctx, propertyID)
		if (err == db.ErrCacheMiss) || needsRefresh {
			uncachedPropertyIDs[propertyID] = count
		} else if err != nil && err != db.ErrNegativeCacheHit {
			slog.ErrorContext(ctx, "Failed to get cached compiled property rules", "propID", propertyID, common.ErrAttr(err))
		}
	}

	slog.DebugContext(ctx, "Uncached property rules", "total", len(batch), "uncached", len(uncachedPropertyIDs))

	if len(uncachedPropertyIDs) == 0 {
		return nil
	}

	// resolve org IDs from properties (they should supposedly be all cached at this time due to other code branches)
	// we have to use non-cached version to later update org rules of active properties
	properties, err := impl.RetrievePropertiesByID(ctx, batch)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties for rules backfill", common.ErrAttr(err))
		return err
	}

	uncachedOrgIDs := make(map[int32]uint, len(properties)/2)
	for _, p := range properties {
		if p.OrgID.Valid {
			if _, seen := uncachedOrgIDs[p.OrgID.Int32]; !seen {
				// see comment for reading compiled property rules above
				_, needsRefresh, err := impl.GetCachedCompiledOrgRules(ctx, p.OrgID.Int32)
				if (err == db.ErrCacheMiss) || needsRefresh {
					uncachedOrgIDs[p.OrgID.Int32] = 1
				} else if err != nil && err != db.ErrNegativeCacheHit {
					slog.ErrorContext(ctx, "Failed to get cached compiled org rules", "orgID", p.OrgID.Int32, common.ErrAttr(err))
				} else if err == nil {
					uncachedOrgIDs[p.OrgID.Int32] = 0
				}
			}
		}
	}

	for orgID, count := range uncachedOrgIDs {
		if count == 0 {
			delete(uncachedOrgIDs, orgID)
		}
	}

	var anyError error

	if propertyRulesMap, err := impl.RetrieveDifficultyRulesByPropertyIDs(ctx, uncachedPropertyIDs); err == nil {
		for propertyID := range uncachedPropertyIDs {
			propRules := propertyRulesMap[propertyID]
			compiled := am.RulesCompiler.Compile(ctx, propRules)
			impl.CacheCompiledPropertyRules(ctx, propertyID, compiled)
		}
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve property difficulty rules", common.ErrAttr(err))
		anyError = err
	}

	if orgRulesMap, err := impl.RetrieveDifficultyRulesByOrgIDs(ctx, uncachedOrgIDs); err == nil {
		for orgID := range uncachedOrgIDs {
			oRules := orgRulesMap[orgID]
			compiled := am.RulesCompiler.Compile(ctx, oRules)
			impl.CacheCompiledOrgRules(ctx, orgID, compiled)
		}
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve org difficulty rules", common.ErrAttr(err))
		anyError = err
	}

	slog.DebugContext(ctx, "Finished processing difficulty rules batch", "properties", len(uncachedPropertyIDs), "orgs", len(uncachedOrgIDs))

	return anyError
}

func (am *AuthMiddleware) originAllowed(r *http.Request, origin string) (bool, []string) {
	return len(origin) > 0, nil
}

func isOriginAllowed(origin string, property *dbgen.Property) bool {
	if len(property.Domain) == 0 {
		return true
	}

	if origin == property.Domain {
		return true
	}

	if common.IsLocalhost(origin) || common.IsSubDomainOrDomain(origin, "localhost") {
		return property.AllowLocalhost
	}

	if property.AllowSubdomains {
		return common.IsSubDomainOrDomain(origin, property.Domain)
	}

	return false
}

func (am *AuthMiddleware) SitekeyOptions(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		sitekey := r.URL.Query().Get(common.ParamSiteKey)
		// don't validate all characters for speed reasons
		if sitekeyLen := len(sitekey); sitekeyLen != db.SitekeyLen {
			slog.Log(ctx, common.LevelTrace, "Sitekey is not valid", "method", r.Method, "length", sitekeyLen)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		ctx = context.WithValue(ctx, common.SitekeyContextKey, sitekey)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (am *AuthMiddleware) refreshPropertyBySitekey(ctx context.Context, sitekey string) {
	timer := time.NewTimer(am.backpressureTimeout)
	defer timer.Stop()

	select {
	// backfill in the background
	case am.SitekeyChan <- sitekey:
	case <-ctx.Done():
		slog.WarnContext(ctx, "Context cancelled for property refresh", "sitekey", sitekey, common.ErrAttr(ctx.Err()))
	case <-timer.C:
		am.Metrics.ObserveEventDropped(common.SitekeyEventType)
	}
}

func (am *AuthMiddleware) refreshForm(ctx context.Context, guid string) {
	timer := time.NewTimer(am.backpressureTimeout)
	defer timer.Stop()

	select {
	case am.FormChan <- guid:
	case <-ctx.Done():
		slog.WarnContext(ctx, "Context cancelled for form refresh", "guid", guid, common.ErrAttr(ctx.Err()))
	case <-timer.C:
		am.Metrics.ObserveEventDropped(common.FormEventType)
	}
}

func (am *AuthMiddleware) refreshAPIKeyLastUsed(ctx context.Context, id int32) {
	timer := time.NewTimer(am.backpressureTimeout)
	defer timer.Stop()

	select {
	case am.APIKeyLastUsedChan <- id:
	case <-ctx.Done():
		slog.WarnContext(ctx, "Context cancelled for API key last used refresh", "id", id, common.ErrAttr(ctx.Err()))
	case <-timer.C:
		am.Metrics.ObserveEventDropped(common.APIKeyEventType)
	}
}

func (am *AuthMiddleware) Sitekey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		origin := r.Header.Get(common.HeaderOrigin)
		if len(origin) == 0 {
			slog.Log(ctx, common.LevelTrace, "Origin header is missing from the request")

			if referer := r.Header.Get(common.HeaderReferer); len(referer) > 0 {
				origin = referer
			} else {
				slog.Log(ctx, common.LevelTrace, "Origin and Referer headers are both missing from the request")
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}

		// we verify sitekey in the underlying DB call
		sitekey := r.URL.Query().Get(common.ParamSiteKey)
		property, needsRefresh, err := am.Store.Impl().GetCachedPropertyBySitekey(ctx, sitekey)
		if err != nil {
			switch err {
			// this will happen when the user does not have such property or it was deleted
			case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted:
				slog.Log(ctx, common.LevelTrace, "Sitekey is not found", "sitekey", len(sitekey), "origin", origin)
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			case db.ErrInvalidInput:
				slog.Log(ctx, common.LevelTrace, "Sitekey is not valid", "sitekey", len(sitekey), "origin", origin)
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			case db.ErrTestProperty:
				// BUMP
			case db.ErrCacheMiss:
				am.refreshPropertyBySitekey(ctx, sitekey)
			default:
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		} else if needsRefresh {
			am.refreshPropertyBySitekey(ctx, sitekey)
		}

		if property != nil {
			if !property.Enabled {
				slog.WarnContext(ctx, "Property is disabled", "propID", property.ID, "domain", property.Domain)
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}

			if originHost, err := common.ParseDomainName(origin); err == nil {
				if !isOriginAllowed(originHost, property) {
					slog.WarnContext(ctx, "Origin is not allowed", "origin", originHost, "domain", property.Domain, "subdomains", property.AllowSubdomains)
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}
			} else {
				slog.WarnContext(ctx, "Failed to parse origin domain name", common.ErrAttr(err))
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}

			if softRestriction, err := am.Limiter.EvaluatePropertyAccess(ctx, property.OrgOwnerID.Int32); err == nil {
				// if user is not an active subscriber, their properties and orgs might still exist but should not serve puzzles
				slog.WarnContext(ctx, "User is limited for property access", "userID", property.OrgOwnerID.Int32, "soft", softRestriction)
				if !softRestriction {
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				} else {
					http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				}
				return
			}

			ctx = context.WithValue(ctx, common.PropertyContextKey, property)
		} else {
			ctx = context.WithValue(ctx, common.SitekeyContextKey, sitekey)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (am *AuthMiddleware) Form(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		guid := r.PathValue(common.ParamForm)
		if !db.CanBeValidSitekey(guid) {
			slog.Log(ctx, common.LevelTrace, "Form GUID is not valid", "length", len(guid))
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		form, needsRefresh, err := am.Store.Impl().GetCachedFormByExternalID(ctx, guid)
		if err != nil {
			switch err {
			case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted:
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			case db.ErrInvalidInput:
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			case db.ErrCacheMiss:
				am.refreshForm(ctx, guid)
			default:
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		} else if needsRefresh {
			am.refreshForm(ctx, guid)
		}

		if form != nil {
			if !form.Enabled {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}

			if property, err := am.Store.Impl().GetCachedPropertyByID(ctx, form.PropertyID); err == nil {
				if !property.Enabled {
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}

				if softRestriction, err := am.Limiter.EvaluatePropertyAccess(ctx, property.OrgOwnerID.Int32); err == nil {
					if !softRestriction {
						http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					} else {
						http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
					}
					return
				}
			}

			ctx = context.WithValue(ctx, common.FormContextKey, form)
		}

		ctx = context.WithValue(ctx, common.FormIDContextKey, guid)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isAPIKeyValid(ctx context.Context, key *dbgen.APIKey, tnow time.Time) bool {
	if key == nil {
		return false
	}

	if !key.Enabled.Valid || !key.Enabled.Bool {
		slog.WarnContext(ctx, "API key is disabled", "keyID", key.ID)
		return false
	}

	if !key.ExpiresAt.Valid || key.ExpiresAt.Time.Before(tnow) {
		slog.WarnContext(ctx, "API key is expired", "keyID", key.ID, "expiresAt", key.ExpiresAt)
		return false
	}

	return true
}

func headerAPIKey(r *http.Request) string {
	return r.Header.Get(common.HeaderAPIKey)
}

func formSecretAPIKey(r *http.Request) string {
	return r.PostFormValue(common.ParamSecret)
}

func (am *AuthMiddleware) APIKey(keyFunc func(r *http.Request) string, scope dbgen.ApiKeyScope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			secret := keyFunc(r)
			if len(secret) != db.SecretLen {
				slog.Log(ctx, common.LevelTrace, "Invalid secret length", "length", len(secret))
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}

			// security assumptions here are that API keys of all legitimate users should be already cached via
			// the backfill routine for puzzles (legitimate verification assumes a previously issued puzzle if on the same server)
			// for everybody else, we rely on rate limiting and delaying DB access to check API key as long as possible.
			// The only exception is when due to routing and/or horizontally scaled servers verify request lands on another node
			apiKey, err := am.Store.Impl().GetCachedAPIKey(ctx, secret)
			if err != nil {
				slog.Log(ctx, common.LevelTrace, "Failed to get cached API key", common.ErrAttr(err))
				switch err {
				case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted:
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				case db.ErrInvalidInput:
					http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
					return
				case db.ErrCacheMiss:
					// do nothing - we postpone accessing DB to after we verify parts of the payload itself
					// we do not backfill API keys like puzzles as we have to check API key validity synchronously
				default:
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
			}

			if apiKey != nil {
				now := time.Now().UTC()
				if !isAPIKeyValid(ctx, apiKey, now) {
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}

				if apiKey.Scope != scope {
					slog.WarnContext(ctx, "API key has invalid scope", "expected", scope, "actual", apiKey.Scope)
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}

				// if user is not an active subscriber, their properties and orgs might still exist but should not allow API
				if softRestriction, err := am.Limiter.EvaluateAPIAccess(ctx, apiKey.UserID.Int32); (err == nil) && !softRestriction {
					slog.WarnContext(ctx, "User is limited for API access", "userID", apiKey.UserID.Int32)
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}

				am.refreshAPIKeyLastUsed(ctx, apiKey.ID)

				ctx = context.WithValue(ctx, common.APIKeyContextKey, apiKey)
			} else {
				ctx = context.WithValue(ctx, common.SecretContextKey, secret)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
