package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	realclientip "github.com/realclientip/realclientip-go"
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

type IPAddrBuckets = leakybucket.Manager[netip.Addr, leakybucket.ConstLeakyBucket[netip.Addr], *leakybucket.ConstLeakyBucket[netip.Addr]]

func NewIPAddrBuckets(maxBuckets int, bucketCap uint32, leakInterval time.Duration) *IPAddrBuckets {
	buckets := leakybucket.NewManager[netip.Addr, leakybucket.ConstLeakyBucket[netip.Addr]](maxBuckets, bucketCap, leakInterval)

	// we setup a separate bucket for "missing" IPs with empty key
	// with a different burst, assuming a misconfiguration on our side
	buckets.SetDefaultBucket(leakybucket.NewConstBucket(netip.Addr{}, 1 /*capacity*/, leakInterval, time.Now()))

	return buckets
}

func NewIPAddrRateLimiter(name, header string, buckets *IPAddrBuckets) *httpRateLimiter[netip.Addr] {
	var strategy realclientip.Strategy

	if len(header) > 0 {
		strategy = realclientip.Must(realclientip.NewSingleIPHeaderStrategy(header))
	} else {
		strategy = realclientip.NewChainStrategy(
			realclientip.Must(realclientip.NewRightmostNonPrivateStrategy("X-Forwarded-For")),
			realclientip.RemoteAddrStrategy{})
	}

	limiter := &httpRateLimiter[netip.Addr]{
		rejectedHandler: defaultRejectedHandler,
		strategy:        strategy,
		buckets:         buckets,
		keyFunc:         func(r *http.Request) netip.Addr { return clientIPAddr(strategy, r) },
	}

	name = strings.ToLower(name)

	var cancelCtx context.Context
	cancelCtx, limiter.cleanupCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, name+"_ip_rate_limiter_cleanup"))
	go limiter.cleanup(cancelCtx)

	return limiter
}
