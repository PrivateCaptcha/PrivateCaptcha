package ratelimit

import (
	"context"
	"log/slog"
	"math"
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
}

type httpRateLimiter[TKey comparable] struct {
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

func (l *httpRateLimiter[TKey]) cleanup(ctx context.Context) {
	// don't over load server on start
	time.Sleep(10 * time.Second)

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
			ctx := context.WithValue(r.Context(), common.RateLimitKeyContextKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			slog.Log(r.Context(), common.LevelTrace, "Rate limiting request", "path", r.URL.Path, "level", addResult.CurrLevel,
				"capacity", addResult.Capacity, "resetAfter", addResult.ResetAfter.String(), "retryAfter", addResult.RetryAfter.String(),
				"found", addResult.Found)
			l.rejectedHandler.ServeHTTP(w, r)
		}
	})
}

func setRateLimitHeaders(w http.ResponseWriter, addResult leakybucket.AddResult) {
	if v := addResult.Capacity; v > 0 {
		w.Header().Add("X-RateLimit-Limit", strconv.Itoa(int(v)))
	}

	if v := addResult.Remaining(); v > 0 {
		w.Header().Add("X-RateLimit-Remaining", strconv.Itoa(int(v)))
	}

	if v := addResult.ResetAfter; v > 0 {
		vi := int(math.Max(1.0, v.Seconds()+0.5))
		w.Header().Add("X-RateLimit-Reset", strconv.Itoa(vi))
	}

	if v := addResult.RetryAfter; v > 0 {
		vi := int(math.Max(1.0, v.Seconds()+0.5))
		w.Header().Add("Retry-After", strconv.Itoa(vi))
	}
}
