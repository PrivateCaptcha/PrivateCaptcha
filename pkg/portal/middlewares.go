package portal

import (
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"golang.org/x/net/xsrftoken"
)

const (
	// by default we are allowing 1 request per 2 seconds from a single client IP address with a {leakyBucketCap} burst
	// for portal we raise these limits for authenticated users and for CDN we have full-on caching
	// for API we have a separate configuration altogether
	// NOTE: this assumes correct configuration of the whole chain of reverse proxies
	// the main problem are NATs/VPNs that make possible for lots of legitimate users to actually come from 1 public IP
	defaultLeakyBucketCap = 10
	defaultLeakInterval   = 2 * time.Second
	// "authenticated" means when we "legitimize" IP address using business logic
	authenticatedBucketCap = 20
	// this effectively means 1 request/second
	authenticatedLeakInterval = 1 * time.Second
)

type XSRFMiddleware struct {
	Key     string
	Timeout time.Duration
}

func (xm *XSRFMiddleware) Token(userID string) string {
	return xsrftoken.Generate(xm.Key, userID, "-")
}

func (xm *XSRFMiddleware) VerifyToken(token, userID string) bool {
	return xsrftoken.ValidFor(token, xm.Key, userID, "-", xm.Timeout)
}

func newDefaultIPAddrBuckets(cfg common.ConfigStore) *ratelimit.IPAddrBuckets {
	const (
		// this is a number of simultaneous users of the portal with different IPs
		maxBuckets = 1_000
	)

	defaultBucketRate := cfg.Get(common.DefaultLeakyBucketRateKey)
	defaultBucketBurst := cfg.Get(common.DefaultLeakyBucketBurstKey)

	return ratelimit.NewIPAddrBuckets(maxBuckets,
		leakybucket.Cap(defaultBucketBurst.Value(), defaultLeakyBucketCap),
		leakybucket.Interval(defaultBucketRate.Value(), defaultLeakInterval))
}

type AuthMiddleware struct {
	rateLimiter ratelimit.HTTPRateLimiter
}

func NewAuthMiddleware(cfg common.ConfigStore) *AuthMiddleware {
	rateLimitHeader := cfg.Get(common.RateLimitHeaderKey).Value()

	return &AuthMiddleware{
		rateLimiter: ratelimit.NewIPAddrRateLimiter("default", rateLimitHeader, newDefaultIPAddrBuckets(cfg)),
	}
}

func (am *AuthMiddleware) RateLimit() func(http.Handler) http.Handler {
	return am.rateLimiter.RateLimit
}

func (am *AuthMiddleware) UpdateConfig(cfg common.ConfigStore) {
	defaultBucketRate := cfg.Get(common.DefaultLeakyBucketRateKey)
	defaultBucketBurst := cfg.Get(common.DefaultLeakyBucketBurstKey)
	am.rateLimiter.UpdateLimits(
		leakybucket.Cap(defaultBucketBurst.Value(), defaultLeakyBucketCap),
		leakybucket.Interval(defaultBucketRate.Value(), defaultLeakInterval))
}

func (am *AuthMiddleware) Shutdown() {
	am.rateLimiter.Shutdown()
}

func (am *AuthMiddleware) UpdateLimits(r *http.Request) {
	am.rateLimiter.Updater(r)(authenticatedBucketCap, authenticatedLeakInterval)
}
