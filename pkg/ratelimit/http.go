package ratelimit

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	realclientip "github.com/realclientip/realclientip-go"
)

var (
	DefaultLimitedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
	})
)

type HTTPRateLimiter struct {
	LimitHandler  http.HandlerFunc
	buckets       *leakybucket.Manager[string, leakybucket.ConstLeakyBucket[string], *leakybucket.ConstLeakyBucket[string]]
	strategy      realclientip.Strategy
	cleanupCancel context.CancelFunc
}

func NewHTTPRateLimiter(header string) (*HTTPRateLimiter, error) {
	strategy, err := realclientip.NewSingleIPHeaderStrategy(header)
	if err != nil {
		return nil, err
	}

	const (
		maxBucketsToKeep = 1_000_000
		// we are allowing 1 request per 2 seconds from a single client IP address with a 10 requests burst
		// NOTE: this assumes correct configuration of the whole chain of reverse proxies
		leakyBucketCap    = 10
		leakRatePerSecond = 0.5
	)

	buckets := leakybucket.NewManager[string, leakybucket.ConstLeakyBucket[string], *leakybucket.ConstLeakyBucket[string]](maxBucketsToKeep, leakyBucketCap, leakRatePerSecond)

	// we setup a separate bucket for "missing" IPs with empty key
	// with a more generous burst, assuming a misconfiguration on our side
	buckets.SetDefaultBucket(leakybucket.NewConstBucket[string]("", 50 /*capacity*/, 1.0 /*leakRatePerSecond*/, time.Now()))

	limiter := &HTTPRateLimiter{
		LimitHandler: DefaultLimitedHandler,
		strategy:     strategy,
		buckets:      buckets,
	}

	var cancelCtx context.Context
	cancelCtx, limiter.cleanupCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "cleanup_rate_limiter"))
	go limiter.cleanup(cancelCtx)

	return limiter, nil
}

func (l *HTTPRateLimiter) Shutdown() {
	l.cleanupCancel()
}

func (l *HTTPRateLimiter) cleanup(ctx context.Context) {
	// don't over load server on start
	time.Sleep(10 * time.Second)

	common.ChunkedCleanup(ctx, 1*time.Second, 10*time.Second, 100 /*chunkSize*/, func(t time.Time, size int) int {
		return l.buckets.Cleanup(t, size)
	})
}

func (l *HTTPRateLimiter) key(r *http.Request) string {
	if l.strategy == nil {
		return ""
	}

	clientIP := l.strategy.ClientIP(r.Header, r.RemoteAddr)

	// We don't want to include the zone in our limiter key
	clientIP, _ = realclientip.SplitHostZone(clientIP)

	return clientIP
}

func (l *HTTPRateLimiter) RateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := l.key(r)

		addResult := l.buckets.Add(key, 1, time.Now())

		setRateLimitHeaders(w, addResult)

		if addResult.Added > 0 {
			next.ServeHTTP(w, r)
		} else {
			h := l.LimitHandler
			if h == nil {
				h = DefaultLimitedHandler
			}

			h.ServeHTTP(w, r)
		}
	}
}

func setRateLimitHeaders(w http.ResponseWriter, addResult leakybucket.AddResult) {
	if v := addResult.Capacity; v > 0 {
		w.Header().Add("X-RateLimit-Limit", strconv.Itoa(int(v)))
	}

	if v := addResult.Remaining(); v > 0 {
		w.Header().Add("X-RateLimit-Remaining", strconv.Itoa(int(v)))
	}

	if v := addResult.ResetAfter; v >= 0 {
		vi := int(math.Max(1.0, v.Seconds()+0.5))
		w.Header().Add("X-RateLimit-Reset", strconv.Itoa(vi))
	}

	if v := addResult.RetryAfter; v >= 0 {
		vi := int(math.Max(1.0, v.Seconds()+0.5))
		w.Header().Add("Retry-After", strconv.Itoa(vi))
	}
}
