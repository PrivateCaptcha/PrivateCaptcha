package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
)

const (
	maxTokenSize            = 300
	apiKeyBurstRequests     = 50
	apiKeyRequestsPerSecond = 10
)

type authMiddleware struct {
	Store          *db.BusinessStore
	ratelimiter    *ratelimit.HTTPRateLimiter
	sitekeyChan    chan string
	batchSize      int
	backfillCancel context.CancelFunc
	privateAPIKey  string
}

func NewAuthMiddleware(getenv func(string) string, store *db.BusinessStore, ratelimiter *ratelimit.HTTPRateLimiter, backfillDelay time.Duration) *authMiddleware {
	const batchSize = 10

	am := &authMiddleware{
		ratelimiter:   ratelimiter,
		Store:         store,
		sitekeyChan:   make(chan string, batchSize),
		batchSize:     batchSize,
		privateAPIKey: getenv("PC_PRIVATE_API_KEY"),
	}

	var backfillCtx context.Context
	backfillCtx, am.backfillCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "auth_backfill"))
	go am.backfillProperties(backfillCtx, backfillDelay)

	return am
}

func (am *authMiddleware) Shutdown() {
	slog.Debug("Shutting down auth middleware")
	close(am.sitekeyChan)
	am.backfillCancel()
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

// the only purpose of this routine is to cache properties
func (am *authMiddleware) backfillProperties(ctx context.Context, delay time.Duration) {
	var batch []string
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

			batch = append(batch, sitekey)

			if len(batch) >= am.batchSize {
				if _, err := am.Store.RetrievePropertiesBySitekey(ctx, batch); err != nil {
					slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
				} else {
					batch = []string{}
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				if _, err := am.Store.RetrievePropertiesBySitekey(ctx, batch); err != nil {
					slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
				} else {
					batch = []string{}
				}
			}
		}
	}

	slog.DebugContext(ctx, "Finished backfilling properties")
}

func (am *authMiddleware) Private(next http.HandlerFunc) http.HandlerFunc {
	return am.ratelimiter.RateLimit(func(w http.ResponseWriter, r *http.Request) {
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
	})
}

func (am *authMiddleware) Sitekey(next http.HandlerFunc) http.HandlerFunc {
	return am.ratelimiter.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			// TODO: Return correct CORS headers
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		sitekey := am.retrieveSiteKey(r)
		if !isSiteKeyValid(sitekey) {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		property, err := am.Store.GetCachedPropertyBySitekey(ctx, sitekey)

		if err != nil {
			switch err {
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

		// TODO: Verify if user is an active subscriber
		// also not blacklisted etc.

		if property != nil {
			ctx = context.WithValue(ctx, common.PropertyContextKey, property)
		} else {
			ctx = context.WithValue(ctx, common.SitekeyContextKey, sitekey)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
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

	return am.ratelimiter.ClientIP(r)
}

func (am *authMiddleware) APIKey(next http.HandlerFunc) http.HandlerFunc {
	return am.ratelimiter.RateLimitKeyFunc(am.apiKeyKeyFunc, func(w http.ResponseWriter, r *http.Request) {
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
				// TODO: Set these limits per subscription plan quota
				// this can be propagated to the APIKey record itself
				am.ratelimiter.UpdateLimits(secret, apiKeyBurstRequests, apiKeyRequestsPerSecond)
			}
		}

		ctx = context.WithValue(ctx, common.APIKeyContextKey, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
