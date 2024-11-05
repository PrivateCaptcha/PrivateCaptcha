package ratelimit

import (
	"context"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	realclientip "github.com/realclientip/realclientip-go"
)

type StringBuckets = leakybucket.Manager[string, leakybucket.ConstLeakyBucket[string], *leakybucket.ConstLeakyBucket[string]]

func NewAPIKeyBuckets() *StringBuckets {
	const (
		maxBucketsToKeep = 1_000
		// we are allowing 1 request per 2 seconds from a single client IP address with a 5 requests burst
		// NOTE: this assumes correct configuration of the whole chain of reverse proxies
		leakyBucketCap    = 5
		leakRatePerSecond = 0.5
		leakInterval      = (1.0 / leakRatePerSecond) * time.Second
	)

	buckets := leakybucket.NewManager[string, leakybucket.ConstLeakyBucket[string], *leakybucket.ConstLeakyBucket[string]](maxBucketsToKeep, leakyBucketCap, leakInterval)

	// we setup a separate bucket for "missing" IPs with empty key
	// with a more generous burst, assuming a misconfiguration on our side
	buckets.SetDefaultBucket(leakybucket.NewConstBucket[string]("", 10 /*capacity*/, 1.0 /*leakRatePerSecond*/, time.Now()))

	return buckets
}

func NewAPIKeyRateLimiter(header string,
	buckets *StringBuckets,
	keyFunc func(r *http.Request) string) HTTPRateLimiter {
	strategy := realclientip.Must(realclientip.NewSingleIPHeaderStrategy(header))

	limiter := &httpRateLimiter[string]{
		rejectedHandler: defaultRejectedHandler,
		strategy:        strategy,
		buckets:         buckets,
		keyFunc: func(r *http.Request) string {
			key := keyFunc(r)
			if len(key) > 0 {
				return key
			}
			return clientIP(strategy, r)
		},
	}

	var cancelCtx context.Context
	cancelCtx, limiter.cleanupCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "cleanup_apikey_rate_limiter"))
	go limiter.cleanup(cancelCtx)

	return limiter
}
