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

func NewAPIKeyBuckets(maxBuckets int, bucketCap uint32, leakInterval time.Duration) *StringBuckets {
	buckets := leakybucket.NewManager[string, leakybucket.ConstLeakyBucket[string]](maxBuckets, bucketCap, leakInterval)

	// we setup a separate bucket for "missing" IPs with empty key
	// with a more generous burst, assuming a misconfiguration on our side
	buckets.SetDefaultBucket(leakybucket.NewConstBucket[string]("", 1 /*capacity*/, leakInterval, time.Now()))

	return buckets
}

func NewAPIKeyRateLimiter(header string,
	buckets *StringBuckets,
	keyFunc func(r *http.Request) string) HTTPRateLimiter {
	var strategy realclientip.Strategy

	if len(header) > 0 {
		strategy = realclientip.Must(realclientip.NewSingleIPHeaderStrategy(header))
	} else {
		strategy = realclientip.NewChainStrategy(
			realclientip.Must(realclientip.NewRightmostNonPrivateStrategy("X-Forwarded-For")),
			realclientip.RemoteAddrStrategy{})
	}

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
		context.WithValue(context.Background(), common.TraceIDContextKey, "apikey_rate_limiter_cleanup"))
	go limiter.cleanup(cancelCtx)

	return limiter
}
