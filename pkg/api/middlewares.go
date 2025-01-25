package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
)

const (
	maxTokenLen  = 300
	maxHeaderLen = 100
	// by default we are allowing 1 request per 2 seconds from a single client IP address with a {leakyBucketCap} burst
	// for portal we raise these limits for authenticated users and for CDN we have full-on caching
	// for API we have a separate configuration altogether
	// NOTE: this assumes correct configuration of the whole chain of reverse proxies
	// the main problem are NATs/VPNs that make possible for lots of legitimate users to actually come from 1 public IP
	defaultLeakyBucketCap = 10
	defaultLeakInterval   = 2 * time.Second
	// for puzzles the logic is that if something becomes popular, there will be a spike, but normal usage should be low
	puzzleLeakyBucketCap = 20
	puzzleLeakInterval   = 1 * time.Second
)

type authMiddleware struct {
	store              *db.BusinessStore
	puzzleRateLimiter  ratelimit.HTTPRateLimiter
	apiKeyRateLimiter  ratelimit.HTTPRateLimiter
	defaultRateLimiter ratelimit.HTTPRateLimiter
	sitekeyChan        chan string
	batchSize          int
	backfillCancel     context.CancelFunc
	privateAPIKey      string
	userLimits         common.Cache[int32, *common.UserLimitStatus]
	presharedHeader    string
	presharedSecret    string
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

func newPuzzleIPAddrBuckets(cfg *config.Config) *ratelimit.IPAddrBuckets {
	const (
		// number of simultaneous different users for /puzzle
		maxBuckets = 1_000_000
	)

	return ratelimit.NewIPAddrBuckets(maxBuckets,
		cfg.LeakyBucketCap(common.AreaPuzzle, puzzleLeakyBucketCap),
		cfg.LeakyBucketInterval(common.AreaPuzzle, puzzleLeakInterval))
}

func newDefaultIPAddrBuckets(cfg *config.Config) *ratelimit.IPAddrBuckets {
	const (
		// this is a number of simultaneous users of the portal with different IPs
		maxBuckets = 1_000
	)

	return ratelimit.NewIPAddrBuckets(maxBuckets,
		cfg.LeakyBucketCap(common.AreaDefault, defaultLeakyBucketCap),
		cfg.LeakyBucketInterval(common.AreaDefault, defaultLeakInterval))
}

func NewAuthMiddleware(cfg *config.Config,
	store *db.BusinessStore,
	userLimits common.Cache[int32, *common.UserLimitStatus],
	backfillDelay time.Duration) *authMiddleware {

	const batchSize = 10
	rateLimitHeader := cfg.RateLimiterHeader()

	am := &authMiddleware{
		puzzleRateLimiter:  ratelimit.NewIPAddrRateLimiter("puzzle", rateLimitHeader, newPuzzleIPAddrBuckets(cfg)),
		defaultRateLimiter: ratelimit.NewIPAddrRateLimiter("default", rateLimitHeader, newDefaultIPAddrBuckets(cfg)),
		store:              store,
		sitekeyChan:        make(chan string, 10*batchSize),
		batchSize:          batchSize,
		privateAPIKey:      cfg.PrivateAPIKey(),
		userLimits:         userLimits,
		presharedHeader:    cfg.PresharedSecretHeader(),
		presharedSecret:    cfg.PresharedSecret(),
	}

	am.apiKeyRateLimiter = ratelimit.NewAPIKeyRateLimiter(
		rateLimitHeader, newAPIKeyBuckets(), am.apiKeyKeyFunc)

	var backfillCtx context.Context
	backfillCtx, am.backfillCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "auth_backfill"))
	go am.backfillProperties(backfillCtx, backfillDelay)

	return am
}

func (am *authMiddleware) DefaultRateLimiter() ratelimit.HTTPRateLimiter {
	return am.defaultRateLimiter
}

func (am *authMiddleware) UpdateConfig(cfg *config.Config) {
	am.puzzleRateLimiter.UpdateLimits(
		cfg.LeakyBucketCap(common.AreaPuzzle, puzzleLeakyBucketCap),
		cfg.LeakyBucketInterval(common.AreaPuzzle, puzzleLeakInterval))

	am.defaultRateLimiter.UpdateLimits(
		cfg.LeakyBucketCap(common.AreaDefault, defaultLeakyBucketCap),
		cfg.LeakyBucketInterval(common.AreaDefault, defaultLeakInterval))
}

func (am *authMiddleware) Shutdown() {
	slog.Debug("Shutting down auth middleware")
	am.apiKeyRateLimiter.Shutdown()
	am.puzzleRateLimiter.Shutdown()
	am.defaultRateLimiter.Shutdown()
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
	am.userLimits.Set(ctx, userID, &common.UserLimitStatus{Status: status, Limit: limit}, db.UserLimitTTL)
	slog.InfoContext(ctx, "Blocked user for auth", "userID", userID, "limit", limit, "status", status)
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

	if subscriptions, err := am.store.RetrieveSubscriptionsByUserIDs(ctx, owners); err == nil {
		for _, s := range subscriptions {
			if !billing.IsSubscriptionActive(s.Subscription.Status) {
				am.userLimits.Set(ctx, s.UserID, &common.UserLimitStatus{Status: s.Subscription.Status, Limit: 0}, db.UserLimitTTL)
				slog.DebugContext(ctx, "Found user with inactive subscription", "userID", s.UserID, "status", s.Subscription.Status)
			} else {
				am.userLimits.SetMissing(ctx, s.UserID, db.UserLimitTTL)
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to check subscriptions", common.ErrAttr(err))
	}

	if users, err := am.store.RetrieveUsersWithoutSubscription(ctx, owners); err == nil {
		for _, u := range users {
			am.userLimits.Set(ctx, u.ID, &common.UserLimitStatus{Status: "", Limit: 0}, db.UserLimitTTL)
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
				if properties, err := am.store.RetrievePropertiesBySitekey(ctx, batch); err != nil {
					slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
				} else {
					batch = make(map[string]struct{})
					am.checkPropertyOwners(ctx, properties)
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				slog.Log(ctx, common.LevelTrace, "Backfilling sitekeys", "count", len(batch), "reason", "timeout")
				if properties, err := am.store.RetrievePropertiesBySitekey(ctx, batch); (err != nil) && (err != db.ErrMaintenance) {
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

// NOTE: unlike other "auth" middlewares, EdgeVerify() does NOT add rate limiting
func (am *authMiddleware) EdgeVerify(allowedHost string) func(http.Handler) http.Handler {
	if (len(allowedHost) == 0) && (len(am.presharedSecret) == 0) {
		return common.NoopMiddleware
	}

	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(allowedHost) > 0 {
				host := r.Host
				if h, _, err := net.SplitHostPort(host); err == nil {
					host = h
				}

				if len(host) == 0 {
					slog.Log(r.Context(), common.LevelTrace, "Host header missing")
					http.Error(w, "", http.StatusUnauthorized)
					return
				}

				if host != allowedHost {
					slog.Log(r.Context(), common.LevelTrace, "Host header mismatch", "expected", allowedHost, "actual", host)
					http.Error(w, "", http.StatusForbidden)
					return
				}
			}

			if len(am.presharedSecret) > 0 {
				secretHeader := r.Header.Get(am.presharedHeader)
				if len(secretHeader) == 0 {
					slog.Log(r.Context(), common.LevelTrace, "Preshared secret missing")
					http.Error(w, "", http.StatusUnauthorized)
					return
				}

				if secretHeader != am.presharedSecret {
					slog.Log(r.Context(), common.LevelTrace, "Preshared secret mismatch", "actual", secretHeader[:maxHeaderLen])
					http.Error(w, "", http.StatusForbidden)
					return
				}
			}

			h.ServeHTTP(w, r)
		})
	}
}

func (am *authMiddleware) retrieveAuthToken(ctx context.Context, r *http.Request) string {
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

func (am *authMiddleware) Private(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		token := am.retrieveAuthToken(ctx, r)
		if len(token) == 0 {
			slog.Log(ctx, common.LevelTrace, "Private auth token is empty")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		if token != am.privateAPIKey {
			slog.WarnContext(ctx, "Invalid authorization token", "token", token[:maxTokenLen])
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		// TODO: Check if we need to rate limit after private key auth
		rateLimited := am.defaultRateLimiter.RateLimit(next)
		rateLimited.ServeHTTP(w, r)
	})
}

func (am *authMiddleware) originAllowed(r *http.Request, origin string) (bool, []string) {
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

func (am *authMiddleware) Sitekey(next http.Handler) http.Handler {
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
			slog.Log(ctx, common.LevelTrace, "Sitekey is not valid")
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
				if !billing.IsSubscriptionActive(status.Status) {
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

func (am *authMiddleware) APIKey(next http.Handler) http.Handler {
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
