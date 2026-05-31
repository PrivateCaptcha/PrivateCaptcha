package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
)

type StubRateLimiter struct {
	Header              string
	UpdateCalls         int
	UpdatedCapacity     leakybucket.TLevel
	UpdatedLeakInterval time.Duration
}

var _ HTTPRateLimiter = (*StubRateLimiter)(nil)

func (srl *StubRateLimiter) context(r *http.Request) context.Context {
	var value string
	if len(srl.Header) > 0 {
		value = r.Header.Get(srl.Header)
	}

	ctx := r.Context()
	if len(value) == 0 {
		slog.Log(ctx, common.LevelTrace, "Test IP address from header is empty")
		value = r.RemoteAddr
	}

	if ip, err := netip.ParseAddr(value); err == nil {
		ctx = context.WithValue(ctx, common.RateLimitKeyContextKey, ip)
	}
	return ctx
}
func (srl *StubRateLimiter) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(srl.context(r)))
	})
}
func (srl *StubRateLimiter) RateLimitExFunc(leakybucket.TLevel, time.Duration) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(srl.context(r)))
		})
	}
}
func (srl *StubRateLimiter) UpdateRequestLimits(r *http.Request, capacity leakybucket.TLevel, leakInterval time.Duration) {
	srl.UpdateCalls++
	srl.UpdatedCapacity = capacity
	srl.UpdatedLeakInterval = leakInterval
}
func (srl *StubRateLimiter) UpdateLimits(capacity leakybucket.TLevel, leakInterval time.Duration) {
	// BUMP
}

func (srl *StubRateLimiter) Clear() {}
func (srl *StubRateLimiter) SaveCache(ctx context.Context, dir string) error {
	return nil
}

func (srl *StubRateLimiter) LoadCache(ctx context.Context, dir string) error { return nil }
