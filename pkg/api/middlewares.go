package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
)

const (
	maxTokenLen = 300
	// for puzzles the logic is that if something becomes popular, there will be a spike, but normal usage should be low
	puzzleLeakyBucketCap = 20
	puzzleLeakInterval   = 1 * time.Second
)

type AuthMiddleware struct {
	store             *db.BusinessStore
	planService       billing.PlanService
	puzzleRateLimiter ratelimit.HTTPRateLimiter
	apiKeyRateLimiter ratelimit.HTTPRateLimiter
	sitekeyChan       chan string
	batchSize         int
	backfillCancel    context.CancelFunc
	userLimits        common.Cache[int32, *common.UserLimitStatus]
	privateAPIKey     common.ConfigItem
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

func NewAuthMiddleware(cfg common.ConfigStore,
	store *db.BusinessStore,
	backfillDelay time.Duration,
	planService billing.PlanService) *AuthMiddleware {
	const maxLimitedUsers = 10_000
	var userLimits common.Cache[int32, *common.UserLimitStatus]
	var err error
	userLimits, err = db.NewMemoryCache[int32, *common.UserLimitStatus](maxLimitedUsers, nil /*missing value*/)
	if err != nil {
		slog.Error("Failed to create memory cache for user limits", common.ErrAttr(err))
		userLimits = db.NewStaticCache[int32, *common.UserLimitStatus](maxLimitedUsers, nil /*missing data*/)
	}

	const batchSize = 10
	rateLimitHeader := cfg.Get(common.RateLimitHeaderKey).Value()

	am := &AuthMiddleware{
		puzzleRateLimiter: ratelimit.NewIPAddrRateLimiter("puzzle", rateLimitHeader, newPuzzleIPAddrBuckets(cfg)),
		store:             store,
		planService:       planService,
		sitekeyChan:       make(chan string, 10*batchSize),
		batchSize:         batchSize,
		userLimits:        userLimits,
		privateAPIKey:     cfg.Get(common.PrivateAPIKeyKey),
	}

	am.apiKeyRateLimiter = ratelimit.NewAPIKeyRateLimiter(
		rateLimitHeader, newAPIKeyBuckets(), am.apiKeyKeyFunc)

	var backfillCtx context.Context
	backfillCtx, am.backfillCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "auth_backfill"))
	go common.ProcessBatchMap(backfillCtx, am.sitekeyChan, backfillDelay, am.batchSize, am.batchSize*100, am.backfillImpl)

	return am
}

func (am *AuthMiddleware) UserLimits() common.Cache[int32, *common.UserLimitStatus] {
	return am.userLimits
}

func (am *AuthMiddleware) UpdateConfig(cfg common.ConfigStore) {
	puzzleBucketRate := cfg.Get(common.PuzzleLeakyBucketRateKey)
	puzzleBucketBurst := cfg.Get(common.PuzzleLeakyBucketBurstKey)
	am.puzzleRateLimiter.UpdateLimits(
		leakybucket.Cap(puzzleBucketBurst.Value(), puzzleLeakyBucketCap),
		leakybucket.Interval(puzzleBucketRate.Value(), puzzleLeakInterval))
}

func (am *AuthMiddleware) Shutdown() {
	slog.Debug("Shutting down auth middleware")
	am.apiKeyRateLimiter.Shutdown()
	am.puzzleRateLimiter.Shutdown()
	am.backfillCancel()
	close(am.sitekeyChan)
}

func (am *AuthMiddleware) PrivateKey() string {
	return am.privateAPIKey.Value()
}

func (am *AuthMiddleware) UnblockUserIfNeeded(ctx context.Context, userID int32, newLimit int64, subscriptionActive bool) {
	if status, err := am.userLimits.Get(ctx, userID); err == nil {
		if (newLimit > status.Limit) || (!status.IsSubscriptionActive && subscriptionActive) {
			slog.InfoContext(ctx, "Unblocking throttled user for auth", "userID", userID, "oldLimit", status.Limit, "newLimit", newLimit,
				"oldActive", status.IsSubscriptionActive, "newActive", subscriptionActive)
			_ = am.userLimits.Delete(ctx, userID)
		} else {
			slog.WarnContext(ctx, "Cannot unblock user for auth", "userID", userID, "oldLimit", status.Limit, "newLimit", newLimit,
				"oldActive", status.IsSubscriptionActive, "newActive", subscriptionActive)
		}
	} else {
		slog.DebugContext(ctx, "User was not blocked", "userID", userID, common.ErrAttr(err))
	}
}

func (am *AuthMiddleware) BlockUser(ctx context.Context, userID int32, limit int64, subscriptionActive bool) {
	if err := am.userLimits.Set(ctx, userID, &common.UserLimitStatus{IsSubscriptionActive: subscriptionActive, Limit: limit}, db.UserLimitTTL); err != nil {
		slog.ErrorContext(ctx, "Failed to block user for auth", "userID", userID, common.ErrAttr(err))
	} else {
		slog.InfoContext(ctx, "Blocked user for auth", "userID", userID, "limit", limit, "active", subscriptionActive)
	}
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

func (am *AuthMiddleware) unknownPropertiesOwners(ctx context.Context, properties []*dbgen.Property) []int32 {
	usersMap := make(map[int32]struct{})
	for _, p := range properties {
		userID := p.OrgOwnerID.Int32

		if _, ok := usersMap[userID]; ok {
			continue
		}

		if _, err := am.userLimits.Get(ctx, userID); err == db.ErrCacheMiss {
			usersMap[userID] = struct{}{}
		}
	}

	result := make([]int32, 0, len(usersMap))
	for key := range usersMap {
		result = append(result, key)
	}

	return result
}

func (am *AuthMiddleware) checkPropertyOwners(ctx context.Context, properties []*dbgen.Property) {
	if len(properties) == 0 {
		return
	}

	owners := am.unknownPropertiesOwners(ctx, properties)
	if len(owners) == 0 {
		slog.DebugContext(ctx, "No new users to check", "properties", len(properties))
		return
	}

	if subscriptions, err := am.store.RetrieveSubscriptionsByUserIDs(ctx, owners); err == nil {
		for _, s := range subscriptions {
			if isActive := am.planService.IsSubscriptionActive(s.Subscription.Status); !isActive {
				_ = am.userLimits.Set(ctx, s.UserID, &common.UserLimitStatus{IsSubscriptionActive: isActive, Limit: 0}, db.UserLimitTTL)
				slog.DebugContext(ctx, "Found user with inactive subscription", "userID", s.UserID, "status", s.Subscription.Status)
			} else {
				_ = am.userLimits.SetMissing(ctx, s.UserID, db.UserLimitTTL)
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to check subscriptions", common.ErrAttr(err))
	}

	if users, err := am.store.RetrieveUsersWithoutSubscription(ctx, owners); err == nil {
		for _, u := range users {
			_ = am.userLimits.Set(ctx, u.ID, &common.UserLimitStatus{IsSubscriptionActive: false, Limit: 0}, db.UserLimitTTL)
		}
	} else {
		slog.ErrorContext(ctx, "Failed to check users without subscriptions", common.ErrAttr(err))
	}
}

// the only purpose of this routine is to cache properties and block users without a subscription
func (am *AuthMiddleware) backfillImpl(ctx context.Context, batch map[string]struct{}) error {
	properties, err := am.store.RetrievePropertiesBySitekey(ctx, batch)

	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
	} else {
		am.checkPropertyOwners(ctx, properties)
	}

	return err
}

func (am *AuthMiddleware) retrieveAuthToken(ctx context.Context, r *http.Request) string {
	authHeader := r.Header.Get(common.HeaderAuthorization)
	if len(authHeader) == 0 {
		slog.WarnContext(ctx, "Authorization header missing")
		return ""
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		slog.WarnContext(ctx, "Invalid authorization header format", "header", authHeader)
		return ""
	}

	return strings.TrimPrefix(authHeader, "Bearer ")
}

func (am *AuthMiddleware) Private(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		token := am.retrieveAuthToken(ctx, r)
		if len(token) == 0 {
			slog.Log(ctx, common.LevelTrace, "Private auth token is empty")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		if token != am.privateAPIKey.Value() {
			slog.WarnContext(ctx, "Invalid authorization token", "token", token[:maxTokenLen])
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		// NOTE: it's not exactly a job for "api key rate limiter", but we moved "default" one to Portal
		rateLimited := am.apiKeyRateLimiter.RateLimit(next)
		rateLimited.ServeHTTP(w, r)
	})
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
	return am.puzzleRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	return am.puzzleRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		property, err := am.store.GetCachedPropertyBySitekey(ctx, sitekey)
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
				am.sitekeyChan <- sitekey
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

			if status, err := am.userLimits.Get(ctx, property.OrgOwnerID.Int32); err == nil {
				// if user is not an active subscriber, their properties and orgs might still exist but should not serve puzzles
				if !status.IsSubscriptionActive {
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
		if apiKey, err := am.store.GetCachedAPIKey(ctx, secret); err == nil {
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
	return am.apiKeyRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		secret := r.Header.Get(common.HeaderAPIKey)
		if len(secret) != db.SecretLen {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		// by now we are ratelimited or cached, so kind of OK to attempt access DB here
		apiKey, err := am.store.RetrieveAPIKey(ctx, secret)
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
				am.apiKeyRateLimiter.Updater(r)(uint32(apiKey.RequestsBurst), time.Duration(interval))
			}
		}

		ctx = context.WithValue(ctx, common.APIKeyContextKey, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}
