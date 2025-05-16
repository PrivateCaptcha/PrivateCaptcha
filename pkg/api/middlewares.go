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
)

type UserLimiter interface {
	CheckProperties(ctx context.Context, properties []*dbgen.Property)
	Evaluate(ctx context.Context, userID int32) (bool, error)
}

type AuthMiddleware struct {
	Store             db.Implementor
	PlanService       billing.PlanService
	PuzzleRateLimiter ratelimit.HTTPRateLimiter
	ApiKeyRateLimiter ratelimit.HTTPRateLimiter
	SitekeyChan       chan string
	BatchSize         int
	BackfillCancel    context.CancelFunc
	Limiter           UserLimiter
}

func newAPIKeyBuckets() *ratelimit.StringBuckets {
	const (
		maxBuckets = 1_000
		// NOTE: these defaults will be adjusted per API key quota almost immediately after verifying API key
		// requests burst
		leakyBucketCap = 20
		// effective 1 request/second
		leakInterval = 1 * time.Second
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
	userLimits common.Cache[int32, any]
}

func (ul *baseUserLimiter) unknownPropertiesOwners(ctx context.Context, properties []*dbgen.Property) []int32 {
	usersMap := make(map[int32]struct{})
	for _, p := range properties {
		userID := p.OrgOwnerID.Int32

		if _, ok := usersMap[userID]; ok {
			continue
		}

		if _, err := ul.userLimits.Get(ctx, userID); err == db.ErrCacheMiss {
			usersMap[userID] = struct{}{}
		}
	}

	result := make([]int32, 0, len(usersMap))
	for key := range usersMap {
		result = append(result, key)
	}

	return result
}

func (ul *baseUserLimiter) CheckProperties(ctx context.Context, properties []*dbgen.Property) {
	if len(properties) == 0 {
		return
	}

	owners := ul.unknownPropertiesOwners(ctx, properties)
	if len(owners) == 0 {
		slog.DebugContext(ctx, "No new users to check", "properties", len(properties))
		return
	}

	if users, err := ul.store.Impl().RetrieveUsersWithoutSubscription(ctx, owners); err == nil {
		violatorsMap := make(map[int32]struct{})
		for _, u := range users {
			_ = ul.userLimits.Set(ctx, u.ID, struct{}{}, db.UserLimitTTL)
			violatorsMap[u.ID] = struct{}{}
		}

		for _, u := range owners {
			if _, found := violatorsMap[u]; !found {
				_ = ul.userLimits.SetMissing(ctx, u, db.UserLimitTTL)
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to check users without subscriptions", common.ErrAttr(err))
	}
}

func (ul *baseUserLimiter) Evaluate(ctx context.Context, userID int32) (bool, error) {
	_, err := ul.userLimits.Get(ctx, userID)
	// "false" because by we only check if user has a subscription at all, we don't verify usage limits
	return false, err
}

func NewUserLimiter(store *db.BusinessStore) *baseUserLimiter {
	const maxLimitedUsers = 10_000
	var userLimits common.Cache[int32, any]
	var err error
	userLimits, err = db.NewMemoryCache[int32, any](maxLimitedUsers, nil /*missing value*/)
	if err != nil {
		slog.Error("Failed to create memory cache for user limits", common.ErrAttr(err))
		userLimits = db.NewStaticCache[int32, any](maxLimitedUsers, nil /*missing data*/)
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
		PuzzleRateLimiter: ratelimit.NewIPAddrRateLimiter("puzzle", rateLimitHeader, newPuzzleIPAddrBuckets(cfg)),
		Store:             store,
		Limiter:           limiter,
		PlanService:       planService,
		SitekeyChan:       make(chan string, 10*batchSize),
		BatchSize:         batchSize,
		BackfillCancel:    func() {},
	}

	am.ApiKeyRateLimiter = ratelimit.NewAPIKeyRateLimiter(
		rateLimitHeader, newAPIKeyBuckets(), am.apiKeyKeyFunc)

	return am
}

func (am *AuthMiddleware) BackfillProperties(backfillDelay time.Duration) {
	var backfillCtx context.Context
	backfillCtx, am.BackfillCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "auth_backfill"))
	go common.ProcessBatchMap(backfillCtx, am.SitekeyChan, backfillDelay, am.BatchSize, am.BatchSize*100, am.backfillImpl)
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
	am.BackfillCancel()
	close(am.SitekeyChan)
}

func isSiteKeyValid(sitekey string) bool {
	if len(sitekey) != db.SitekeyLen {
		return false
	}

	for _, c := range sitekey {
		//nolint:staticcheck
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}

	return true
}

// the only purpose of this routine is to cache properties and block users without a subscription
func (am *AuthMiddleware) backfillImpl(ctx context.Context, batch map[string]struct{}) error {
	if properties, err := am.Store.Impl().RetrievePropertiesBySitekey(ctx, batch); err == nil {
		am.Limiter.CheckProperties(ctx, properties)
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
		return err
	}

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

func (am *AuthMiddleware) Sitekey(next http.Handler) http.Handler {
	return am.PuzzleRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		origin := r.Header.Get("Origin")
		if len(origin) == 0 {
			slog.Log(ctx, common.LevelTrace, "Origin header is missing from the request")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		sitekey := r.URL.Query().Get(common.ParamSiteKey)
		if !isSiteKeyValid(sitekey) {
			slog.Log(ctx, common.LevelTrace, "Sitekey is not valid", "method", r.Method)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		// NOTE: there's a potential problem here if the property is still cached then
		// we will not backfill and, thus, verify the subscription validity of the user
		property, err := am.Store.Impl().GetCachedPropertyBySitekey(ctx, sitekey)
		if err != nil {
			switch err {
			// this will happen when the user does not have such property or it was deleted
			case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted:
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			case db.ErrInvalidInput:
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

func (am *AuthMiddleware) isAPIKeyValid(ctx context.Context, key *dbgen.APIKey, tnow time.Time) bool {
	if key == nil {
		return false
	}

	if !key.Enabled.Valid || !key.Enabled.Bool {
		slog.WarnContext(ctx, "API key is disabled")
		return false
	}

	if !key.ExpiresAt.Valid || key.ExpiresAt.Time.Before(tnow) {
		slog.WarnContext(ctx, "API key is expired", "expiresAt", key.ExpiresAt)
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
			if am.isAPIKeyValid(ctx, apiKey, tnow) {
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

		// by now we are ratelimited or cached, so kind of OK to attempt access DB here
		apiKey, err := am.Store.Impl().RetrieveAPIKey(ctx, secret)
		if err != nil {
			switch err {
			case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted:
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			case db.ErrInvalidInput:
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			default:
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
			return
		}

		now := time.Now().UTC()
		if !am.isAPIKeyValid(ctx, apiKey, now) {
			// am.Cache.SetMissing(ctx, secret, negativeCacheDuration)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		} else {
			// rate limiter key will be the {secret} itself _only_ when we are cached
			// which means if it's not, then we have just fetched the record from DB
			// when rate limiting is cleaned up (due to inactivity) we should still be able to access on defaults
			if rateLimiterKey, ok := ctx.Value(common.RateLimitKeyContextKey).(string); ok && (rateLimiterKey != secret) {
				interval := float64(time.Second) / apiKey.RequestsPerSecond
				am.ApiKeyRateLimiter.Updater(r)(uint32(apiKey.RequestsBurst), time.Duration(interval))
			}
		}

		ctx = context.WithValue(ctx, common.APIKeyContextKey, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}
