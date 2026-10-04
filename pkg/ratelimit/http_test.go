package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestHTTPRateLimiterBasic(t *testing.T) {
	// Create rate limiter with very low capacity (2 requests)
	buckets := NewIPAddrBuckets(100, 2, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	wrapped := limiter.RateLimit(handler)

	// First request should succeed
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	w1 := httptest.NewRecorder()
	wrapped.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request should succeed, got status %d", w1.Code)
	}

	// Second request should succeed
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:12345"
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Second request should succeed, got status %d", w2.Code)
	}

	// Third request should be rate limited
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.1:12345"
	w3 := httptest.NewRecorder()
	wrapped.ServeHTTP(w3, req3)

	if w3.Code != http.StatusTooManyRequests {
		t.Errorf("Third request should be rate limited, got status %d", w3.Code)
	}
}

func TestHTTPRateLimiterDifferentIPs(t *testing.T) {
	// Create rate limiter with capacity of 1
	buckets := NewIPAddrBuckets(100, 1, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := limiter.RateLimit(handler)

	// First IP - first request should succeed
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	w1 := httptest.NewRecorder()
	wrapped.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request from IP1 should succeed, got status %d", w1.Code)
	}

	// Second IP - first request should also succeed (different bucket)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "10.0.0.2:1234"
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("First request from IP2 should succeed, got status %d", w2.Code)
	}

	// First IP - second request should be rate limited
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "10.0.0.1:1234"
	w3 := httptest.NewRecorder()
	wrapped.ServeHTTP(w3, req3)

	if w3.Code != http.StatusTooManyRequests {
		t.Errorf("Second request from IP1 should be rate limited, got status %d", w3.Code)
	}
}

func TestHTTPRateLimiterHeaders(t *testing.T) {
	buckets := NewIPAddrBuckets(100, 5, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := limiter.RateLimit(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:5000"
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	// Check that rate limit headers are set
	if w.Header().Get("X-Ratelimit-Limit") == "" {
		t.Error("Expected X-Ratelimit-Limit header to be set")
	}
}

func TestHTTPRateLimiterRateLimitExFunc(t *testing.T) {
	// Create rate limiter with default high capacity
	buckets := NewIPAddrBuckets(100, 100, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Use RateLimitExFunc with very low custom capacity
	wrapped := limiter.RateLimitExFunc(1, 1*time.Second)(handler)

	// First request should succeed
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "172.16.0.1:9000"
	w1 := httptest.NewRecorder()
	wrapped.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request should succeed, got status %d", w1.Code)
	}

	// Second request should be rate limited (custom capacity of 1)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "172.16.0.1:9000"
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should be rate limited with custom capacity, got status %d", w2.Code)
	}
}

func TestHTTPRateLimiterUpdateLimits(t *testing.T) {
	buckets := NewIPAddrBuckets(100, 10, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	// Update global limits to very low capacity
	limiter.UpdateLimits(1, 1*time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := limiter.RateLimit(handler)

	// First request should succeed
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.10.1:1234"
	w1 := httptest.NewRecorder()
	wrapped.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request should succeed, got status %d", w1.Code)
	}

	// Second request should be rate limited (updated capacity of 1)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.10.1:1234"
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should be rate limited after UpdateLimits, got status %d", w2.Code)
	}
}

func TestHTTPRateLimiterContextKey(t *testing.T) {
	buckets := NewIPAddrBuckets(100, 10, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	var contextKeyFound bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Context().Value(common.RateLimitKeyContextKey)
		contextKeyFound = key != nil
		w.WriteHeader(http.StatusOK)
	})

	wrapped := limiter.RateLimit(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.10.10.10:1234"
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if !contextKeyFound {
		t.Error("Expected rate limit key to be set in request context")
	}
}

func TestHTTPRateLimiterCustomHeader(t *testing.T) {
	buckets := NewIPAddrBuckets(100, 2, 1*time.Second)
	limiter := NewIPAddrRateLimiter("X-Real-IP", buckets)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := limiter.RateLimit(handler)

	// First request with custom header IP
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "127.0.0.1:1234" // This will be ignored
	req1.Header.Set("X-Real-IP", "203.0.113.1")
	w1 := httptest.NewRecorder()
	wrapped.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request should succeed, got status %d", w1.Code)
	}

	// Second request from same custom header IP
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "127.0.0.1:5678" // Different RemoteAddr but same X-Real-IP
	req2.Header.Set("X-Real-IP", "203.0.113.1")
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Second request should succeed (capacity 2), got status %d", w2.Code)
	}

	// Third request from same custom header IP should be rate limited
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "127.0.0.1:9999"
	req3.Header.Set("X-Real-IP", "203.0.113.1")
	w3 := httptest.NewRecorder()
	wrapped.ServeHTTP(w3, req3)

	if w3.Code != http.StatusTooManyRequests {
		t.Errorf("Third request should be rate limited, got status %d", w3.Code)
	}
}

func TestHTTPRateLimiterRetryAfterHeader(t *testing.T) {
	buckets := NewIPAddrBuckets(100, 1, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := limiter.RateLimit(handler)

	// First request consumes the bucket
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.99.1:1234"
	w1 := httptest.NewRecorder()
	wrapped.ServeHTTP(w1, req1)

	// Second request should be rate limited and have Retry-After header
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.99.1:1234"
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should be rate limited, got status %d", w2.Code)
	}

	retryAfter := w2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Expected Retry-After header when rate limited")
	}
}

func TestHTTPRateLimiterUpdateRequestLimitsUnderflow(t *testing.T) {
	buckets := NewIPAddrBuckets(100, 10, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := limiter.RateLimitExFunc(10, 1*time.Second)(handler) // new buckets get cap=10
	addr := "198.51.100.7:31337"

	// Step 1: prime the rate limit key in the context by sending one admitted
	// request through the wrapper, mirroring how the verify chain seeds the
	// per-IP key before the handler calls UpdateRequestLimits.
	seedReq := httptest.NewRequest("GET", "/test", nil)
	seedReq.RemoteAddr = addr
	seedW := httptest.NewRecorder()
	wrapped.ServeHTTP(seedW, seedReq)
	if seedW.Code != http.StatusOK {
		t.Fatalf("seed request should be admitted, got %d", seedW.Code)
	}

	// Step 2: fill the rest of the initial burst so level == capacity (10).
	for i := 0; i < 9; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d should be admitted, got %d", i+1, w.Code)
		}
	}

	// Step 3: lower the per-key capacity below the current level (10 -> 5),
	// exactly as pkg/api/server.go does with apiKey.RequestsBurst for a
	// free-tier key. Use the seeded context key path to avoid the fallback
	// "key not found" warning and exercise the realistic code path.
	upgradeReq := httptest.NewRequest("GET", "/test", nil)
	upgradeReq.RemoteAddr = addr
	// Replay the key into the request context exactly like the middleware does.
	upgradeReq = upgradeReq.WithContext(context.WithValue(upgradeReq.Context(), common.RateLimitKeyContextKey,
		clientIPAddr(limiter.strategy, upgradeReq)))
	limiter.UpdateRequestLimits(upgradeReq, 5, 1*time.Second)

	// Step 4: the bucket is now over capacity (level=10 > cap=5) prior to the
	// clamp fix. The next request MUST be rejected (429), not admitted (200)
	// due to a uint32-wrapped negative Added value.
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = addr
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("BUG: request admitted (200) after capacity lowered below current level; expected 429")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after capacity downgrade, got %d", w.Code)
	}

	// Step 5: the bucket must keep rejecting subsequent requests (level ==
	// capacity == 5 after the clamp), i.e. the bypass must not persist.
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("GET", "/test", nil)
		r.RemoteAddr = addr
		ww := httptest.NewRecorder()
		wrapped.ServeHTTP(ww, r)
		if ww.Code != http.StatusTooManyRequests {
			t.Errorf("follow-up request %d should be rate limited, got %d", i, ww.Code)
		}
	}
}

func TestHTTPRateLimiterUpdateRequestLimitsRaiseCapAdmitsBurst(t *testing.T) {
	buckets := NewIPAddrBuckets(100, 10, 1*time.Second)
	limiter := NewIPAddrRateLimiter("", buckets)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := limiter.RateLimitExFunc(10, 1*time.Second)(handler) // new buckets get cap=10
	addr := "198.51.100.7:31337"

	// Prime + fill to the chain default cap of 10.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d should be admitted, got %d", i, w.Code)
		}
	}

	// Raise the cap above the chain default, like pkg/api/form_proxy.go does
	// (capacity = RequestsPerMinute + 10 >= 11).
	raiseReq := httptest.NewRequest("GET", "/test", nil)
	raiseReq.RemoteAddr = addr
	raiseReq = raiseReq.WithContext(context.WithValue(raiseReq.Context(), common.RateLimitKeyContextKey,
		clientIPAddr(limiter.strategy, raiseReq)))
	limiter.UpdateRequestLimits(raiseReq, 20, 1*time.Second)

	// With the cap raised to 20 while level is 10, the next request should be
	// admitted (Added > 0): the raise path must not regress.
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = addr
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("request after cap raise should be admitted, got %d", w.Code)
	}
}
