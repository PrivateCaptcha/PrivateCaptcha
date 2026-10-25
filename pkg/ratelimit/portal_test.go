package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/justinas/alice"
)

// raiseSharedBucket mimics the effect of pkg/portal/server.go's s.private
// middleware: it raises the shared per-IP bucket to the authenticated (20, 1s)
// defaults via UpdateRequestLimits. Applied after the shared limiter view that
// created the bucket, exactly as in the real portal chain.
func raiseSharedBucket(shared HTTPRateLimiter, capacity uint32, leak time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			shared.UpdateRequestLimits(r, capacity, leak)
			next.ServeHTTP(w, r)
		})
	}
}

const portalTestClientAddr = "198.51.100.42:9000"

func newPortalCompositionRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/org/example/form/example/test", nil)
	r.RemoteAddr = portalTestClientAddr
	return r
}

// TestSharedLimiterViewAfterUpdateIsShadowedByExistingBucket documents the
// root cause of the portal test-form rate-limit bug: when a
// RateLimitExFunc(2, 5s) view is appended to the SAME shared limiter that an
// earlier view already created a bucket on (and that s.private then raised to
// 20/1s via UpdateRequestLimits), the (2, 5s) init parameters are silently
// ignored for the existing bucket. Requests are charged against the permissive
// 20/1s bucket and X-RateLimit-Limit reports 20; the intended 2-per-5s burst
// is never enforced. This test pins that pre-fix behavior so a future change to
// leakybucket.Manager.AddEx semantics (which only apply init params to NEW
// buckets) is detected and deliberately revisited.
func TestSharedLimiterViewAfterUpdateIsShadowedByExistingBucket(t *testing.T) {
	// Real shared limiter with the portal public-chain default (10, 2s).
	shared := NewIPAddrRateLimiter("", NewIPAddrBuckets(1_000, 10, 2*time.Second))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Order mirrors the buggy portal chain: shared view creates the bucket,
	// raise it to (20, 1s), then the (2, 5s) view runs on the SAME shared limiter.
	chain := alice.New(
		shared.RateLimitExFunc(10, 2*time.Second),
		raiseSharedBucket(shared, 20, 1*time.Second),
		shared.RateLimitExFunc(2, 5*time.Second),
	).Then(handler)

	const total = 10
	admitted := 0
	lastLimitHeader := ""
	for i := 0; i < total; i++ {
		w := httptest.NewRecorder()
		chain.ServeHTTP(w, newPortalCompositionRequest())
		if w.Code == http.StatusOK {
			admitted++
		}
		if h := w.Header().Get("X-RateLimit-Limit"); h != "" {
			lastLimitHeader = h
		}
	}

	// The (2, 5s) parameters are inert: the shared bucket was raised to 20/1s
	// before the view ran, so all 10 requests fit within the permissive burst.
	if admitted != total {
		t.Errorf("shadowed shared limiter admitted %d/%d (pre-fix behavior); "+
			"if AddEx now applies init params to existing buckets, the leakybucket "+
			"semantics changed and this test plus manager_test.go must be revisited", admitted, total)
	}
	if lastLimitHeader != "20" {
		t.Errorf("expected X-RateLimit-Limit=20 from the raised shared bucket, got %q", lastLimitHeader)
	}
}

// TestDedicatedLimiterViewAfterUpdateEnforcesConfiguredBurst verifies the fix:
// a DEDICATED limiter instance (separate leakybucket.Manager) enforces its
// configured (2, 5s) burst even when composed after a shared limiter whose
// bucket was raised to the permissive (20, 1s) authenticated defaults. The
// dedicated manager owns its own buckets, so the (2, 5s) parameters always apply.
func TestDedicatedLimiterViewAfterUpdateEnforcesConfiguredBurst(t *testing.T) {
	shared := NewIPAddrRateLimiter("", NewIPAddrBuckets(1_000, 10, 2*time.Second))
	// Dedicated limiter with the intended (2, 5s) test-form defaults; separate manager.
	dedicated := NewIPAddrRateLimiter("", NewIPAddrBuckets(1_000, 2, 5*time.Second))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Order mirrors the fixed portal chain: shared view creates+raises the
	// shared bucket, then the dedicated limiter runs on its own manager.
	chain := alice.New(
		shared.RateLimitExFunc(10, 2*time.Second),
		raiseSharedBucket(shared, 20, 1*time.Second),
		dedicated.RateLimit,
	).Then(handler)

	const total = 10
	admitted := 0
	rejected := 0
	lastLimitHeader := ""
	for i := 0; i < total; i++ {
		w := httptest.NewRecorder()
		chain.ServeHTTP(w, newPortalCompositionRequest())
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
		t.Errorf("expected X-RateLimit-Limit=2 from the dedicated limiter, got %q", lastLimitHeader)
	}
}

// TestDedicatedLimiterIsolationFromSharedBucket verifies the dedicated
// test-form limiter's buckets are physically independent of the shared
// limiter's buckets: saturating the shared limiter must not consume any of the
// dedicated limiter's burst budget, and vice versa.
func TestDedicatedLimiterIsolationFromSharedBucket(t *testing.T) {
	shared := NewIPAddrRateLimiter("", NewIPAddrBuckets(1_000, 5, 1*time.Second))
	dedicated := NewIPAddrRateLimiter("", NewIPAddrBuckets(1_000, 2, 5*time.Second))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Exhaust the shared limiter (5 requests from the same IP fill its bucket).
	sharedWrapped := shared.RateLimit(handler)
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		sharedWrapped.ServeHTTP(w, newPortalCompositionRequest())
		if w.Code != http.StatusOK {
			t.Fatalf("shared limiter request %d should be admitted, got %d", i, w.Code)
		}
	}
	// Next shared request must be rejected, proving the shared bucket is full.
	sharedNext := httptest.NewRecorder()
	sharedWrapped.ServeHTTP(sharedNext, newPortalCompositionRequest())
	if sharedNext.Code != http.StatusTooManyRequests {
		t.Fatalf("shared limiter should reject the 6th request, got %d", sharedNext.Code)
	}

	// The dedicated limiter must still admit its full (2, 5s) burst even though
	// the shared limiter is saturated, because their buckets are independent.
	dedicatedWrapped := dedicated.RateLimit(handler)
	admitted := 0
	for i := 0; i < 4; i++ {
		w := httptest.NewRecorder()
		dedicatedWrapped.ServeHTTP(w, newPortalCompositionRequest())
		if w.Code == http.StatusOK {
			admitted++
		}
	}
	if admitted != 2 {
		t.Errorf("expected dedicated limiter to admit 2 independently of the saturated shared limiter, got %d", admitted)
	}
}
