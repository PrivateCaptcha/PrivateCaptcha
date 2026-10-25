package portal

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/justinas/alice"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
)

const testFormTestClientAddr = "198.51.100.7:31337"

// newTestFormTestRequest builds a POST request that the IP rate limiter
// resolves to a single, stable client address (no X-Forwarded-For header),
// mirroring the shape of a portal test-form submission.
func newTestFormTestRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/org/example/form/example/test", nil)
	r.RemoteAddr = testFormTestClientAddr
	return r
}

// privateRaise mimics the effect of pkg/portal/server.go's s.private middleware
// on the shared per-IP limiter: it raises the existing bucket to the
// authenticated (20, 1s) defaults via UpdateRequestLimits. It is intentionally
// applied *after* the shared limiter view that creates the bucket, exactly as
// in the real portal chain, so the dedicated test-form limiter runs last.
func privateRaise(shared ratelimit.HTTPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			shared.UpdateRequestLimits(r, 20, 1*time.Second)
			next.ServeHTTP(w, r)
		})
	}
}

// TestNewTestFormRateLimiterEnforcesConfiguredLimit verifies the dedicated
// test-form limiter instance actually enforces the configured (2, 5s) burst
// when used in isolation, i.e. the parameters the buggy shared-limiter wiring
// silently ignored are honored by the dedicated manager.
func TestNewTestFormRateLimiterEnforcesConfiguredLimit(t *testing.T) {
	limiter := NewTestFormRateLimiter("")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := limiter.RateLimit(handler)

	admitted := 0
	rejected := 0
	limitHeader := ""
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, newTestFormTestRequest())
		switch w.Code {
		case http.StatusOK:
			admitted++
		case http.StatusTooManyRequests:
			rejected++
		}
		if h := w.Header().Get("X-RateLimit-Limit"); h != "" {
			limitHeader = h
		}
	}

	if admitted != 2 {
		t.Errorf("expected exactly 2 admitted requests (burst of 2), got %d", admitted)
	}
	if rejected != 3 {
		t.Errorf("expected 3 rejected requests after the burst, got %d", rejected)
	}
	if limitHeader != "2" {
		t.Errorf("expected X-RateLimit-Limit=2, got %q", limitHeader)
	}
}

// TestTestFormRateLimitViewDedicatedAfterAuthRaisesShared reproduces the portal
// middleware ordering that guards the test-form endpoint (public shared limiter
// -> s.private raises the shared bucket to 20/1s -> dedicated test-form
// limiter) and verifies the dedicated limiter enforces the intended 2-per-5s
// burst despite the earlier shared bucket being raised to the permissive 20/1s
// defaults. This is the core regression test for the fix: with the pre-fix
// wiring (testFormRateLimiter := s.RateLimiter.RateLimitExFunc(2, 5s) on the
// shared limiter), the (2, 5s) parameters were shadowed and 10/10 requests were
// admitted with X-RateLimit-Limit=20.
func TestTestFormRateLimitViewDedicatedAfterAuthRaisesShared(t *testing.T) {
	// Real shared limiter, matching the portal public-chain defaults (10, 2s).
	shared := ratelimit.NewIPAddrRateLimiter("", ratelimit.NewIPAddrBuckets(1_000, 10, 2*time.Second))

	// Build the dedicated test-form view via the same Server helper the route
	// registration uses. Server has no TestFormRateLimiter configured, so this
	// exercises the fallback path (a dedicated manager with the (2, 5s) defaults).
	// s.RateLimiter is wired to the same shared limiter used by the chain so that a
	// regression to s.RateLimiter.RateLimitExFunc(2, 5s) (the pre-fix wiring) would
	// be exercised against the shared bucket raised by privateRaise, admitting all
	// requests and failing this test.
	s := &Server{RateLimiter: shared}
	testFormView := s.testFormRateLimitView()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Order mirrors the portal chain: shared limiter view creates the bucket,
	// s.private raises it to (20, 1s), then the dedicated test-form view runs.
	chain := alice.New(shared.RateLimitExFunc(10, 2*time.Second), privateRaise(shared), testFormView).Then(handler)

	const total = 10
	admitted := 0
	rejected := 0
	lastLimitHeader := ""
	for i := 0; i < total; i++ {
		w := httptest.NewRecorder()
		chain.ServeHTTP(w, newTestFormTestRequest())
		switch w.Code {
		case http.StatusOK:
			admitted++
		case http.StatusTooManyRequests:
			rejected++
		}
		if h := w.Header().Get("X-RateLimit-Limit"); h != "" {
			lastLimitHeader = h
		}
	}

	if admitted != 2 {
		t.Errorf("expected exactly 2 admitted requests (intended 2-per-5s burst), got %d", admitted)
	}
	if rejected != total-2 {
		t.Errorf("expected %d rejected requests, got %d", total-2, rejected)
	}
	if lastLimitHeader != "2" {
		t.Errorf("expected X-RateLimit-Limit=2 (dedicated limiter wins, set last), got %q", lastLimitHeader)
	}
}

// TestTestFormRateLimitViewUsesConfiguredLimiter verifies that when the Server
// is wired with an explicit TestFormRateLimiter (as cmd/server/main.go does in
// production), the helper uses that instance rather than the fallback, so the
// production client-IP header strategy is honored.
func TestTestFormRateLimitViewUsesConfiguredLimiter(t *testing.T) {
	configured := NewTestFormRateLimiter("X-Real-IP")
	s := &Server{TestFormRateLimiter: configured}

	view := s.testFormRateLimitView()
	if view == nil {
		t.Fatal("expected non-nil rate limit view")
	}

	// Sanity: the configured limiter enforces the (2, 5s) burst.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := view(handler)

	admitted := 0
	for i := 0; i < 4; i++ {
		r := httptest.NewRequest(http.MethodPost, "/org/x/form/y/test", nil)
		r.RemoteAddr = "203.0.113.9:5000"
		r.Header.Set("X-Real-IP", "203.0.113.9")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, r)
		if w.Code == http.StatusOK {
			admitted++
		}
	}
	if admitted != 2 {
		t.Errorf("expected configured dedicated limiter to admit exactly 2, got %d", admitted)
	}
}
