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

func clientIPAddr(strategy realclientip.Strategy, r *http.Request) netip.Addr {
	ipStr := clientIP(strategy, r)
	if len(ipStr) == 0 {
		return netip.Addr{}
	}

	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to parse netip.Addr", "ip", clientIP, common.ErrAttr(err))
		return netip.Addr{}
	}

	return addr
}

func NewIPAddrRateLimiter(header string) HTTPRateLimiter {
	strategy := realclientip.Must(realclientip.NewSingleIPHeaderStrategy(header))

	const (
		maxBucketsToKeep = 1_000_000
		// we are allowing 1 request per 2 seconds from a single client IP address with a 5 requests burst
		// NOTE: this assumes correct configuration of the whole chain of reverse proxies
		leakyBucketCap    = 5
		leakRatePerSecond = 0.5
		leakInterval      = (1.0 / leakRatePerSecond) * time.Second
	)

	buckets := leakybucket.NewManager[netip.Addr, leakybucket.ConstLeakyBucket[netip.Addr], *leakybucket.ConstLeakyBucket[netip.Addr]](maxBucketsToKeep, leakyBucketCap, leakInterval)

	// we setup a separate bucket for "missing" IPs with empty key
	// with a more generous burst, assuming a misconfiguration on our side
	buckets.SetDefaultBucket(leakybucket.NewConstBucket[netip.Addr](netip.Addr{}, 50 /*capacity*/, 1.0 /*leakRatePerSecond*/, time.Now()))

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
