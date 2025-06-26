package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
)

const (
	// for puzzles the logic is that if something becomes popular, there will be a spike, but normal usage should be low
	puzzleLeakyBucketCap = 20
	puzzleLeakInterval   = 1 * time.Second
	userLimitTTL         = 1 * time.Hour
	userLimitRefresh     = 3 * time.Hour
)

type UserLimiter interface {
	CheckUsers(ctx context.Context, users map[int32]uint) error
	Evaluate(ctx context.Context, userID int32) (bool, error)
}

type AuthMiddleware struct {
	Store                 db.Implementor
	PlanService           billing.PlanService
	PuzzleRateLimiter     ratelimit.HTTPRateLimiter
	ApiKeyRateLimiter     ratelimit.HTTPRateLimiter
	SitekeyChan           chan string
	UsersChan             chan int32
	BatchSize             int
	SitekeyBackfillCancel context.CancelFunc
	UsersBackfillCancel   context.CancelFunc
	Limiter               UserLimiter
	// this is a simple way to control negative cache spam, disabled by default
	NegativeSitekeyThreshold uint
}

func newAPIKeyBuckets() *ratelimit.StringBuckets {
	const (
		maxBuckets = 1_000
		// NOTE: these defaults will be adjusted per API key quota almost immediately after verifying API key
		// requests burst
		leakyBucketCap = 10
		// effective 0.5 rps
		leakInterval = 2 * time.Second
	)

	return ratelimit.NewAPIKeyBuckets(maxBuckets, leakyBucketCap, leakInterval)
}

func newPuzzleIPAddrBuckets(cfg common.ConfigStore) *ratelimit.IPAddrBuckets {
	const (
		// number of simultaneous different users for /puzzle
		maxBuckets = 1_000_000
	)

	puzzleBucketRate := cfg.Get(common.PuzzleLeakyBucketRateKey)
	puzzleBucketBurst := cfg.Get(common.PuzzleLeakyBucketBurstKey)

	return ratelimit.NewIPAddrBuckets(maxBuckets,
		leakybucket.Cap(puzzleBucketBurst.Value(), puzzleLeakyBucketCap),
		leakybucket.Interval(puzzleBucketRate.Value(), puzzleLeakInterval))
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
		violatorsMap := make(map[int32]struct{})
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

func (ul *baseUserLimiter) Evaluate(ctx context.Context, userID int32) (bool, error) {
	_, err := ul.userLimits.Get(ctx, userID)
	// "false" because by we only check if user has a subscription at all, we don't verify usage limits
	return false, err
}

func NewUserLimiter(store db.Implementor) *baseUserLimiter {
	const maxLimitedUsers = 10_000
	var userLimits common.Cache[int32, bool]
	var err error
	// missing TTL should be equal to "usual" TTL here because it has the same meaning (we mark user has no violation)
	userLimits, err = db.NewMemoryCache[int32, bool](maxLimitedUsers, false /*missing value*/, userLimitTTL, userLimitRefresh, userLimitTTL)
	if err != nil {
		slog.Error("Failed to create memory cache for user limits", common.ErrAttr(err))
		userLimits = db.NewStaticCache[int32, bool](maxLimitedUsers, false /*missing data*/)
	}

	return &baseUserLimiter{
		userLimits: userLimits,
		store:      store,
	}
}

func NewAuthMiddleware(cfg common.ConfigStore,
	store db.Implementor,
	limiter UserLimiter,
	planService billing.PlanService) *AuthMiddleware {
	const batchSize = 10
	rateLimitHeader := cfg.Get(common.RateLimitHeaderKey).Value()

	am := &AuthMiddleware{
		PuzzleRateLimiter:     ratelimit.NewIPAddrRateLimiter("puzzle", rateLimitHeader, newPuzzleIPAddrBuckets(cfg)),
		Store:                 store,
		Limiter:               limiter,
		PlanService:           planService,
		SitekeyChan:           make(chan string, 100*batchSize),
		UsersChan:             make(chan int32, 10*batchSize),
		BatchSize:             batchSize,
		SitekeyBackfillCancel: func() {},
		UsersBackfillCancel:   func() {},
	}

	am.ApiKeyRateLimiter = ratelimit.NewAPIKeyRateLimiter(
		rateLimitHeader, newAPIKeyBuckets(), am.apiKeyKeyFunc)

	return am
}

func (am *AuthMiddleware) StartBackfill(backfillDelay time.Duration) {
	var sitekeyBackfillCtx context.Context
	sitekeyBackfillCtx, am.SitekeyBackfillCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "sitekey_backfill"))
	go common.ProcessBatchMap(sitekeyBackfillCtx, am.SitekeyChan, backfillDelay, am.BatchSize, am.BatchSize*100, am.backfillSitekeyImpl)

	var usersBackfillCtx context.Context
	usersBackfillCtx, am.UsersBackfillCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "users_backfill"))
	// NOTE: we use the same backfill delay because users processing is slower and sitekey channel will block on it
	go common.ProcessBatchMap(usersBackfillCtx, am.UsersChan, backfillDelay, am.BatchSize, am.BatchSize*10, am.backfillUsersImpl)
}

func (am *AuthMiddleware) UpdateConfig(cfg common.ConfigStore) {
	puzzleBucketRate := cfg.Get(common.PuzzleLeakyBucketRateKey)
	puzzleBucketBurst := cfg.Get(common.PuzzleLeakyBucketBurstKey)
	am.PuzzleRateLimiter.UpdateLimits(
		leakybucket.Cap(puzzleBucketBurst.Value(), puzzleLeakyBucketCap),
		leakybucket.Interval(puzzleBucketRate.Value(), puzzleLeakInterval))
}

func (am *AuthMiddleware) Shutdown() {
	slog.Debug("Shutting down auth middleware")
	am.ApiKeyRateLimiter.Shutdown()
	am.PuzzleRateLimiter.Shutdown()
	am.SitekeyBackfillCancel()
	am.UsersBackfillCancel()
	close(am.SitekeyChan)
	close(am.UsersChan)
}

// we cache properties and send owners down the background pipeline
func (am *AuthMiddleware) backfillSitekeyImpl(ctx context.Context, batch map[string]uint) error {
	properties, err := am.Store.Impl().RetrievePropertiesBySitekey(ctx, batch, am.NegativeSitekeyThreshold)
	if err == nil {
		for _, p := range properties {
			if p.OrgOwnerID.Valid {
				am.UsersChan <- p.OrgOwnerID.Int32
			}
			if p.CreatorID.Valid && (!p.OrgOwnerID.Valid || (p.CreatorID.Int32 != p.OrgOwnerID.Int32)) {
				am.UsersChan <- p.CreatorID.Int32
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", "count", len(batch), common.ErrAttr(err))
	}

	return err
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

func (am *AuthMiddleware) originAllowed(r *http.Request, origin string) (bool, []string) {
	return len(origin) > 0, nil
}

func isOriginAllowed(origin string, property *dbgen.Property) bool {
	if common.IsLocalhost(origin) {
		return property.AllowLocalhost
	}

	if property.AllowSubdomains {
		return common.IsSubDomainOrDomain(origin, property.Domain)
	}

	return origin == property.Domain
}

func (am *AuthMiddleware) SitekeyOptions(next http.Handler) http.Handler {
	return am.PuzzleRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		sitekey := r.URL.Query().Get(common.ParamSiteKey)
		// don't validate all characters for speed reasons
		if len(sitekey) != db.SitekeyLen {
			slog.Log(ctx, common.LevelTrace, "Sitekey is not valid", "method", r.Method)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		ctx = context.WithValue(ctx, common.SitekeyContextKey, sitekey)

		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}

func (am *AuthMiddleware) refreshPropertyBySitekey(sitekey string) {
	// backfill in the background
	am.SitekeyChan <- sitekey
}

func (am *AuthMiddleware) Sitekey(next http.Handler) http.Handler {
	return am.PuzzleRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		origin := r.Header.Get("Origin")
		if len(origin) == 0 {
			slog.Log(ctx, common.LevelTrace, "Origin header is missing from the request")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		// we verify sitekey in underlying DB call
		sitekey := r.URL.Query().Get(common.ParamSiteKey)
		property, err := am.Store.Impl().GetCachedPropertyBySitekey(ctx, sitekey, am.refreshPropertyBySitekey)
		if err != nil {
			switch err {
			// this will happen when the user does not have such property or it was deleted
			case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted:
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			case db.ErrInvalidInput:
				slog.Log(ctx, common.LevelTrace, "Sitekey is not valid", "method", r.Method)
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			case db.ErrTestProperty:
				// BUMP
			case db.ErrCacheMiss:
				// backfill in the background
				am.SitekeyChan <- sitekey
			default:
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		if property != nil {
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

			if softRestriction, err := am.Limiter.Evaluate(ctx, property.OrgOwnerID.Int32); err == nil {
				// if user is not an active subscriber, their properties and orgs might still exist but should not serve puzzles
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
	}))
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

func (am *AuthMiddleware) apiKeyKeyFunc(r *http.Request) string {
	ctx := r.Context()
	secret := r.Header.Get(common.HeaderAPIKey)

	if len(secret) == db.SecretLen {
		if apiKey, err := am.Store.Impl().GetCachedAPIKey(ctx, secret); err == nil {
			tnow := time.Now().UTC()
			// we want to keep this check here to rate limit even expired valid keys (which can be leaked)
			if isAPIKeyValid(ctx, apiKey, tnow) {
				// if we know API key is valid, we ratelimit by API key which has different limits
				return secret
			}
		}
	}

	return ""
}

func (am *AuthMiddleware) APIKey(next http.Handler) http.Handler {
	return am.ApiKeyRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		secret := r.Header.Get(common.HeaderAPIKey)
		if len(secret) != db.SecretLen {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		// security assumptions here are that API keys of all legitimate users should be already cached via
		// the backfill routine for puzzles (legitimate verification assumes a previously issued puzzle if on the same server)
		// for everybody else, we rely on rate limiting and delaying DB access to check API key as long as possible.
		// The only exception is when due to routing and/or horizontally scaled servers verify request lands on another node
		apiKey, err := am.Store.Impl().GetCachedAPIKey(ctx, secret)
		if err != nil {
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
			} else {
				// rate limiter key will be the {secret} itself _only_ when we are cached (which means valid API key)
				// which means if it's not, then we will recheck via "delayed" mechanism of OwnerIDSource
				// when rate limiting is cleaned up (due to inactivity) we should still be able to access on defaults
				if rateLimiterKey, ok := ctx.Value(common.RateLimitKeyContextKey).(string); ok && len(rateLimiterKey) > 0 {
					interval := float64(time.Second) / apiKey.RequestsPerSecond
					am.ApiKeyRateLimiter.UpdateRequestLimits(r, uint32(apiKey.RequestsBurst), time.Duration(interval))
				}
			}
			ctx = context.WithValue(ctx, common.APIKeyContextKey, apiKey)
		} else {
			ctx = context.WithValue(ctx, common.SecretContextKey, secret)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}
