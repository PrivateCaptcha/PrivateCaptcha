package ratelimit

import (
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
)

type StubRateLimiter struct {
}

var _ HTTPRateLimiter = (*StubRateLimiter)(nil)

func (srl *StubRateLimiter) Shutdown() {
	// BUMP
}
func (srl *StubRateLimiter) RateLimit(next http.Handler) http.Handler {
	return next
}
func (srl *StubRateLimiter) Updater(r *http.Request) leakybucket.LimitUpdaterFunc {
	return func(capacity leakybucket.TLevel, leakInterval time.Duration) {
		// BUMP
	}
}
func (srl *StubRateLimiter) UpdateLimits(capacity leakybucket.TLevel, leakInterval time.Duration) {
	// BUMP
}
