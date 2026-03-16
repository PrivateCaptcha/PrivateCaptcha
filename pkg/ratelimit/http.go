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
	RateLimit(next http.Handler) http.Handler
	// this API allows to create a "view" (in SQL sense) to underlying rate limiter with other defaults for new buckets
	RateLimitExFunc(initCapacity leakybucket.TLevel, initLeakInterval time.Duration) func(next http.Handler) http.Handler
	UpdateRequestLimits(r *http.Request, capacity leakybucket.TLevel, leakInterval time.Duration)
	UpdateLimits(capacity leakybucket.TLevel, leakInterval time.Duration)
}

type httpRateLimiter[TKey comparable] struct {
	rejectedHandler http.HandlerFunc
	buckets         *leakybucket.Manager[TKey, leakybucket.ConstLeakyBucket[TKey], *leakybucket.ConstLeakyBucket[TKey]]
	strategy        realclientip.Strategy
	keyFunc         func(r *http.Request) TKey
	// prevent stampeding herds problem with random increase to Retry-After
	retryJitterPercent float64
}

var _ HTTPRateLimiter = (*httpRateLimiter[string])(nil)

func (l *httpRateLimiter[TKey]) UpdateLimits(capacity leakybucket.TLevel, leakInterval time.Duration) {
	l.buckets.SetGlobalLimits(capacity, leakInterval)
}

func (l *httpRateLimiter[TKey]) RateLimitExFunc(initCapacity leakybucket.TLevel, initLeakInterval time.Duration) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := l.keyFunc(r)

			addResult := l.buckets.AddEx(key, 1, time.Now(), initCapacity, initLeakInterval)

			l.setRateLimitHeaders(w, addResult)

			if addResult.Added > 0 {
				//slog.Log(r.Context(), common.LevelTrace, "Allowing request", "ratelimiter", l.name,
				//	"key", key, "host", r.Host, "path", r.URL.Path, "method", r.Method,
				//	"level", addResult.CurrLevel, "capacity", addResult.Capacity, "found", addResult.Found)

				ctx := context.WithValue(r.Context(), common.RateLimitKeyContextKey, key)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				slog.Log(r.Context(), common.LevelTrace, "Rate limiting request",
					"key", key, "host", r.Host, "path", r.URL.Path, "method", r.Method,
					"level", addResult.CurrLevel, "capacity", addResult.Capacity, "resetAfter", addResult.ResetAfter.String(),
					"retryAfter", addResult.RetryAfter.String(), "found", addResult.Found)
				l.rejectedHandler.ServeHTTP(w, r)
			}
		})
	}
}

// this is a twin of RateLimitEx() that is using `buckets` defaults
func (l *httpRateLimiter[TKey]) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := l.keyFunc(r)

		addResult := l.buckets.Add(key, 1, time.Now())

		l.setRateLimitHeaders(w, addResult)

		if addResult.Added > 0 {
			//slog.Log(r.Context(), common.LevelTrace, "Allowing request", "ratelimiter", l.name,
			//	"key", key, "host", r.Host, "path", r.URL.Path, "method", r.Method,
			//	"level", addResult.CurrLevel, "capacity", addResult.Capacity, "found", addResult.Found)

			ctx := context.WithValue(r.Context(), common.RateLimitKeyContextKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			slog.Log(r.Context(), common.LevelTrace, "Rate limiting request",
				"key", key, "host", r.Host, "path", r.URL.Path, "method", r.Method,
				"level", addResult.CurrLevel, "capacity", addResult.Capacity, "resetAfter", addResult.ResetAfter.String(),
				"retryAfter", addResult.RetryAfter.String(), "found", addResult.Found)
			l.rejectedHandler.ServeHTTP(w, r)
		}
	})
}

func (l *httpRateLimiter[TKey]) UpdateRequestLimits(r *http.Request, capacity leakybucket.TLevel, leakInterval time.Duration) {
	ctx := r.Context()
	key, ok := ctx.Value(common.RateLimitKeyContextKey).(TKey)
	if !ok {
		slog.WarnContext(ctx, "Rate limit key not found in http request context")
		key = l.keyFunc(r)
	}

	l.buckets.Update(key, capacity, leakInterval, time.Now())
}

func (l *httpRateLimiter[TKey]) setRateLimitHeaders(w http.ResponseWriter, addResult leakybucket.AddResult) {
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
		jitter := randv2.Float64() * l.retryJitterPercent
		seconds := v.Seconds() * (1.0 + jitter)
		vi := int(math.Max(1.0, seconds+0.5))
		headers[retryAfterHeader] = []string{strconv.Itoa(vi)}
	}
}
