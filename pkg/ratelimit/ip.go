package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	realclientip "github.com/realclientip/realclientip-go"
)

const (
	// we are allowing 1 request per 2 seconds from a single client IP address with a {leakyBucketCap} requests burst
	// NOTE: this assumes correct configuration of the whole chain of reverse proxies
	leakyBucketCap    = 5
	leakRatePerSecond = 0.5
	leakInterval      = (1.0 / leakRatePerSecond) * time.Second
	// "authenticated" means when we "legitimize" IP address using business logic
	AuthenticatedBucketCap = 8
	// this effectively means leakRate = 0.75/second
	AuthenticatedLeakInterval = 750 * time.Millisecond
)

func clientIPAddr(strategy realclientip.Strategy, r *http.Request) netip.Addr {
	ipStr := clientIP(strategy, r)
	if len(ipStr) == 0 {
		slog.WarnContext(r.Context(), "Empty IP address used for rate limiting")
		return netip.Addr{}
	}

	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to parse netip.Addr", "ip", ipStr, common.ErrAttr(err))
		return netip.Addr{}
	}

	return addr
}

func NewIPAddrRateLimiter(header string) *httpRateLimiter[netip.Addr] {
	strategy := realclientip.Must(realclientip.NewSingleIPHeaderStrategy(header))

	const maxBucketsToKeep = 1_000_000

	buckets := leakybucket.NewManager[netip.Addr, leakybucket.ConstLeakyBucket[netip.Addr]](maxBucketsToKeep, leakyBucketCap, leakInterval)

	// we setup a separate bucket for "missing" IPs with empty key
	// with a different burst, assuming a misconfiguration on our side
	buckets.SetDefaultBucket(leakybucket.NewConstBucket(netip.Addr{}, 2 /*capacity*/, leakInterval, time.Now()))

	limiter := &httpRateLimiter[netip.Addr]{
		rejectedHandler: defaultRejectedHandler,
		strategy:        strategy,
		buckets:         buckets,
		keyFunc:         func(r *http.Request) netip.Addr { return clientIPAddr(strategy, r) },
	}

	var cancelCtx context.Context
	cancelCtx, limiter.cleanupCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "cleanup_ip_rate_limiter"))
	go limiter.cleanup(cancelCtx)

	return limiter
}
