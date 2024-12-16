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
	maxTokenSize = 300
)

type authMiddleware struct {
	Store             *db.BusinessStore
	ipRateLimiter     ratelimit.HTTPRateLimiter
	apiKeyRateLimiter ratelimit.HTTPRateLimiter
	apiKeyBuckets     *ratelimit.StringBuckets
	sitekeyChan       chan string
	batchSize         int
	backfillCancel    context.CancelFunc
	privateAPIKey     string
	userLimits        common.Cache[int32, *common.UserLimitStatus]
}

func NewAuthMiddleware(getenv func(string) string,
	store *db.BusinessStore,
	ipRateLimiter ratelimit.HTTPRateLimiter,
	userLimits common.Cache[int32, *common.UserLimitStatus],
	backfillDelay time.Duration) *authMiddleware {
	const batchSize = 10

	am := &authMiddleware{
		ipRateLimiter: ipRateLimiter,
		apiKeyBuckets: ratelimit.NewAPIKeyBuckets(),
		Store:         store,
		sitekeyChan:   make(chan string, 3*batchSize),
		batchSize:     batchSize,
		privateAPIKey: getenv("PC_PRIVATE_API_KEY"),
		userLimits:    userLimits,
	}

	am.apiKeyRateLimiter = ratelimit.NewAPIKeyRateLimiter(
		getenv(common.ConfigRateLimitHeader), am.apiKeyBuckets, am.apiKeyKeyFunc)

	var backfillCtx context.Context
	backfillCtx, am.backfillCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "auth_backfill"))
	go am.backfillProperties(backfillCtx, backfillDelay)

	return am
}

func (am *authMiddleware) Shutdown() {
	slog.Debug("Shutting down auth middleware")
	am.apiKeyRateLimiter.Shutdown()
	am.backfillCancel()
	close(am.sitekeyChan)
}

func (am *authMiddleware) UnblockUserIfNeeded(ctx context.Context, userID int32, newLimit int64, newStatus string) {
	if status, err := am.userLimits.Get(ctx, userID); err == nil {
		if (newLimit > status.Limit) || (!billing.IsSubscriptionActive(status.Status) && billing.IsSubscriptionActive(newStatus)) {
			slog.InfoContext(ctx, "Unblocking throttled user for auth", "userID", userID, "oldLimit", status.Limit, "newLimit", newLimit,
				"oldStatus", status.Status, "newStatus", status)
			am.userLimits.Delete(ctx, userID)
		} else {
			slog.WarnContext(ctx, "Cannot unblock user for auth", "userID", userID, "oldLimit", status.Limit, "newLimit", newLimit,
				"oldStatus", status.Status, "newStatus", status)
		}
	} else {
		slog.DebugContext(ctx, "User was not blocked", "userID", userID, common.ErrAttr(err))
	}
}

func (am *authMiddleware) BlockUser(ctx context.Context, userID int32, limit int64, status string) {
	am.userLimits.Set(ctx, userID, &common.UserLimitStatus{Status: status, Limit: limit})
	slog.InfoContext(ctx, "Blocked user for auth", "userID", userID, "limit", limit, "status", status)
}

func (am *authMiddleware) retrieveSiteKey(r *http.Request) string {
	if r.Method == http.MethodGet {
		return r.URL.Query().Get(common.ParamSiteKey)
	} else if r.Method == http.MethodPost {
		return r.FormValue(common.ParamSiteKey)
	}

	return ""
}

func (am *authMiddleware) retrieveSecret(r *http.Request) string {
	if r.Method == http.MethodPost {
		return r.FormValue(common.ParamSecret)
	}

	return ""
}

func isSiteKeyValid(sitekey string) bool {
	if len(sitekey) != db.SitekeyLen {
		return false
	}

	for _, c := range sitekey {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}

	return true
}

func (am *authMiddleware) unknownPropertiesOwners(ctx context.Context, properties []*dbgen.Property) []int32 {
	usersMap := make(map[int32]bool)
	for _, p := range properties {
		userID := p.OrgOwnerID.Int32

		if _, ok := usersMap[userID]; ok {
			continue
		}

		if _, err := am.userLimits.Get(ctx, userID); err == db.ErrCacheMiss {
			usersMap[userID] = true
		}
	}

	result := make([]int32, 0, len(usersMap))
	for key := range usersMap {
		result = append(result, key)
	}

	return result
}

func (am *authMiddleware) checkPropertyOwners(ctx context.Context, properties []*dbgen.Property) {
	if len(properties) == 0 {
		return
	}

	owners := am.unknownPropertiesOwners(ctx, properties)
	if len(owners) == 0 {
		slog.DebugContext(ctx, "No new users to check", "properties", len(properties))
		return
	}

	if subscriptions, err := am.Store.RetrieveSubscriptionsByUserIDs(ctx, owners); err == nil {
		for _, s := range subscriptions {
			if !billing.IsSubscriptionActive(s.Subscription.Status) {
				am.userLimits.Set(ctx, s.UserID, &common.UserLimitStatus{Status: s.Subscription.Status, Limit: 0})
				slog.DebugContext(ctx, "Found user with inactive subscription", "userID", s.UserID, "status", s.Subscription.Status)
			} else {
				am.userLimits.SetMissing(ctx, s.UserID)
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to check subscriptions", common.ErrAttr(err))
	}

	if users, err := am.Store.RetrieveUsersWithoutSubscription(ctx, owners); err == nil {
		for _, u := range users {
			am.userLimits.Set(ctx, u.ID, &common.UserLimitStatus{Status: "", Limit: 0})
		}
	} else {
		slog.ErrorContext(ctx, "Failed to check users without subscriptions", common.ErrAttr(err))
	}
}

// the only purpose of this routine is to cache properties and block users without a subscription
func (am *authMiddleware) backfillProperties(ctx context.Context, delay time.Duration) {
	batch := map[string]struct{}{}
	slog.DebugContext(ctx, "Backfilling properties", "interval", delay.String())

	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false

		case sitekey, ok := <-am.sitekeyChan:
			if !ok {
				running = false
				break
			}

			batch[sitekey] = struct{}{}

			if len(batch) >= am.batchSize {
				slog.Log(ctx, common.LevelTrace, "Backfilling sitekeys", "count", len(batch), "reason", "batch")
				if properties, err := am.Store.RetrievePropertiesBySitekey(ctx, batch); err != nil {
					slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
				} else {
					batch = make(map[string]struct{})
					am.checkPropertyOwners(ctx, properties)
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				slog.Log(ctx, common.LevelTrace, "Backfilling sitekeys", "count", len(batch), "reason", "timeout")
				if properties, err := am.Store.RetrievePropertiesBySitekey(ctx, batch); (err != nil) && (err != db.ErrMaintenance) {
					slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
				} else {
					batch = make(map[string]struct{})
					am.checkPropertyOwners(ctx, properties)
				}
			}
		}
	}

	slog.DebugContext(ctx, "Finished backfilling properties")
}

func (am *authMiddleware) Private(next http.Handler) http.Handler {
	return am.ipRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authHeader := r.Header.Get(common.HeaderAuthorization)
		if authHeader == "" {
			slog.WarnContext(ctx, "Authorization header missing")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			slog.WarnContext(ctx, "Invalid authorization header format", "header", authHeader)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")

		if token != am.privateAPIKey {
			slog.WarnContext(ctx, "Invalid authorization token", "token", token[:maxTokenSize])
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}))
}

func (am *authMiddleware) originAllowed(origin string) bool {
	return len(origin) > 0
}

func (am *authMiddleware) Sitekey(next http.Handler) http.Handler {
	return am.ipRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		origin := r.Header.Get("Origin")
		if len(origin) == 0 {
			slog.Log(ctx, common.LevelTrace, "Origin header is missing from the request")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		sitekey := am.retrieveSiteKey(r)
		if !isSiteKeyValid(sitekey) {
			slog.Log(ctx, common.LevelTrace, "Sitekey is not valid")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		// NOTE: there's a potential problem here if the property is still cached then
		// we will not backfill and, thus, verify the subscription validity of the user
		property, err := am.Store.GetCachedPropertyBySitekey(ctx, sitekey)
		if err != nil {
			switch err {
			// this will happen when the user does not have such property or it was deleted
			case db.ErrNegativeCacheHit, db.ErrRecordNotFound, db.ErrSoftDeleted:
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			case db.ErrInvalidInput:
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
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
				if originHost != property.Domain {
					slog.WarnContext(ctx, "Origin header to domain mismatch", "origin", originHost, "domain", property.Domain)
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
				if !billing.IsSubscriptionActive(status.Status) {
					http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
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

func (am *authMiddleware) isAPIKeyValid(ctx context.Context, key *dbgen.APIKey, tnow time.Time) bool {
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

func (am *authMiddleware) apiKeyKeyFunc(r *http.Request) string {
	ctx := r.Context()
	secret := am.retrieveSecret(r)

	if len(secret) == db.SecretLen {
		if apiKey, err := am.Store.GetCachedAPIKey(ctx, secret); err == nil {
			tnow := time.Now().UTC()
			if am.isAPIKeyValid(ctx, apiKey, tnow) {
				// if we know API key is valid, we ratelimit by API key which has different limits
				return secret
			}
		}
	}

	return ""
}

func (am *authMiddleware) APIKey(next http.Handler) http.Handler {
	return am.apiKeyRateLimiter.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		secret := am.retrieveSecret(r)
		if len(secret) != db.SecretLen {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		// by now we are ratelimited or cached, so kind of OK to attempt access DB here
		apiKey, err := am.Store.RetrieveAPIKey(ctx, secret)
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
			if rateLimiterKey, ok := ctx.Value(common.RateLimitKeyContextKey).(string); ok && (rateLimiterKey != secret) {
				am.updateLimits(secret, uint32(apiKey.RequestsBurst), apiKey.RequestsPerSecond)
			}
		}

		ctx = context.WithValue(ctx, common.APIKeyContextKey, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}

func (am *authMiddleware) updateLimits(key string, capacity leakybucket.TLevel, rateLimitPerSecond float64) bool {
	interval := float64(time.Second) / rateLimitPerSecond
	return am.apiKeyBuckets.Update(key, capacity, time.Duration(interval))
}
