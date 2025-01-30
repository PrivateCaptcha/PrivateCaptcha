package ratelimit

import (
	"context"
	"log/slog"
	"math"
	randv2 "math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	realclientip "github.com/realclientip/realclientip-go"
)

var (
	defaultRejectedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
	})

	rateLimitHeader          = http.CanonicalHeaderKey("X-RateLimit-Limit")
	rateLimitRemainingHeader = http.CanonicalHeaderKey("X-RateLimit-Remaining")
	rateLimitResetHeader     = http.CanonicalHeaderKey("X-RateLimit-Reset")
	retryAfterHeader         = http.CanonicalHeaderKey("Retry-After")
)

func clientIP(strategy realclientip.Strategy, r *http.Request) string {
	if strategy == nil {
		return ""
	}

	clientIP := strategy.ClientIP(r.Header, r.RemoteAddr)

	// We don't want to include the zone in our limiter key
	clientIP, _ = realclientip.SplitHostZone(clientIP)

	return clientIP
}

type HTTPRateLimiter interface {
	Shutdown()
	RateLimit(next http.Handler) http.Handler
	Updater(r *http.Request) leakybucket.LimitUpdaterFunc
	UpdateLimits(capacity leakybucket.TLevel, leakInterval time.Duration)
}

type httpRateLimiter[TKey comparable] struct {
	name            string
	rejectedHandler http.HandlerFunc
	buckets         *leakybucket.Manager[TKey, leakybucket.ConstLeakyBucket[TKey], *leakybucket.ConstLeakyBucket[TKey]]
	strategy        realclientip.Strategy
	cleanupCancel   context.CancelFunc
	keyFunc         func(r *http.Request) TKey
}

var _ HTTPRateLimiter = (*httpRateLimiter[string])(nil)

func (l *httpRateLimiter[TKey]) Shutdown() {
	l.cleanupCancel()
}

func (l *httpRateLimiter[TKey]) UpdateLimits(capacity leakybucket.TLevel, leakInterval time.Duration) {
	l.buckets.SetGlobalLimits(capacity, leakInterval)
}

func (l *httpRateLimiter[TKey]) cleanup(ctx context.Context) {
	const jitter = 4 * time.Second
	// don't overload server on start
	time.Sleep(10*time.Second + time.Duration(randv2.Int64N(int64(jitter))))

	common.ChunkedCleanup(ctx, 1*time.Second, 10*time.Second, 100 /*chunkSize*/, func(t time.Time, size int) int {
		return l.buckets.Cleanup(ctx, t, size, nil)
	})
}

func (l *httpRateLimiter[TKey]) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := l.keyFunc(r)

		addResult := l.buckets.Add(key, 1, time.Now())

		setRateLimitHeaders(w, addResult)

		if addResult.Added > 0 {
			//slog.Log(r.Context(), common.LevelTrace, "Allowing request", "ratelimiter", l.name,
			//	"key", key, "host", r.Host, "path", r.URL.Path, "method", r.Method,
			//	"level", addResult.CurrLevel, "capacity", addResult.Capacity, "found", addResult.Found)

			ctx := context.WithValue(r.Context(), common.RateLimitKeyContextKey, key)
			ctx = context.WithValue(ctx, common.RateLimitLevelContextKey, addResult.CurrLevel)
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			slog.Log(r.Context(), common.LevelTrace, "Rate limiting request", "ratelimiter", l.name,
				"key", key, "host", r.Host, "path", r.URL.Path, "method", r.Method,
				"level", addResult.CurrLevel, "capacity", addResult.Capacity, "resetAfter", addResult.ResetAfter.String(),
				"retryAfter", addResult.RetryAfter.String(), "found", addResult.Found)
			l.rejectedHandler.ServeHTTP(w, r)
		}
	})
}

func (l *httpRateLimiter[TKey]) UpdaterByKey(key TKey) leakybucket.LimitUpdaterFunc {
	return l.buckets.UpdaterFunc(key)
}

func (l *httpRateLimiter[TKey]) Updater(r *http.Request) leakybucket.LimitUpdaterFunc {
	ctx := r.Context()
	if key, ok := ctx.Value(common.RateLimitKeyContextKey).(TKey); ok {
		return l.buckets.UpdaterFunc(key)
	} else {
		slog.WarnContext(ctx, "Rate limit key not found in request context")
	}

	return func(capacity leakybucket.TLevel, leakInterval time.Duration) {
		// BUMP
	}
}

func setRateLimitHeaders(w http.ResponseWriter, addResult leakybucket.AddResult) {
	headers := w.Header()

	if v := addResult.Capacity; v > 0 {
		headers[rateLimitHeader] = []string{strconv.Itoa(int(v))}
	}

	if v := addResult.Remaining(); v > 0 {
		headers[rateLimitRemainingHeader] = []string{strconv.Itoa(int(v))}
	}

	if v := addResult.ResetAfter; v > 0 {
		vi := int(math.Max(1.0, v.Seconds()+0.5))
		headers[rateLimitResetHeader] = []string{strconv.Itoa(vi)}
	}

	if v := addResult.RetryAfter; v > 0 {
		vi := int(math.Max(1.0, v.Seconds()+0.5))
		headers[retryAfterHeader] = []string{strconv.Itoa(vi)}
	}
}
