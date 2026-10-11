package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
)

func TestApplyAPIKeyRateLimits(t *testing.T) {
	tests := []struct {
		name         string
		apiKey       *dbgen.APIKey
		wantCalls    int
		wantCapacity uint32
		wantInterval time.Duration
	}{
		{
			name:         "positive finite rps updates limits",
			apiKey:       &dbgen.APIKey{RequestsPerSecond: 10.0, RequestsBurst: 50},
			wantCalls:    1,
			wantCapacity: 50,
			wantInterval: 100 * time.Millisecond,
		},
		{
			name:         "fractional positive rps updates limits",
			apiKey:       &dbgen.APIKey{RequestsPerSecond: 0.5, RequestsBurst: 5},
			wantCalls:    1,
			wantCapacity: 5,
			wantInterval: 2 * time.Second,
		},
		{
			name:      "zero rps skipped",
			apiKey:    &dbgen.APIKey{RequestsPerSecond: 0.0, RequestsBurst: 50},
			wantCalls: 0,
		},
		{
			name:      "negative rps skipped",
			apiKey:    &dbgen.APIKey{RequestsPerSecond: -5.0, RequestsBurst: 50},
			wantCalls: 0,
		},
		{
			name:      "NaN rps skipped",
			apiKey:    &dbgen.APIKey{RequestsPerSecond: math.NaN(), RequestsBurst: 50},
			wantCalls: 0,
		},
		{
			name:      "plus infinity rps skipped",
			apiKey:    &dbgen.APIKey{RequestsPerSecond: math.Inf(1), RequestsBurst: 50},
			wantCalls: 0,
		},
		{
			name:      "minus infinity rps skipped",
			apiKey:    &dbgen.APIKey{RequestsPerSecond: math.Inf(-1), RequestsBurst: 50},
			wantCalls: 0,
		},
		{
			// Regression: a negative RequestsBurst (int32) must be rejected
			// by the burst guard, not cast to uint32 (which would underflow
			// to 4294967295 and poison the leaky-bucket capacity). The RPS
			// value here is valid (10.0), so pre-fix the unguarded
			// uint32(apiKey.RequestsBurst) cast reached UpdateRequestLimits
			// with capacity = uint32(int32(-1)) = 4294967295.
			name:      "negative burst skipped",
			apiKey:    &dbgen.APIKey{RequestsPerSecond: 10.0, RequestsBurst: -1},
			wantCalls: 0,
		},
		{
			// Regression: RequestsBurst == 0 casts to uint32(0), which sets
			// the leaky-bucket capacity to 0 and rejects every request (a
			// per-IP lockout). The burst guard must reject this the same way
			// a degenerate RPS is rejected.
			name:      "zero burst skipped",
			apiKey:    &dbgen.APIKey{RequestsPerSecond: 10.0, RequestsBurst: 0},
			wantCalls: 0,
		},
		{
			name:      "nil api key skipped",
			apiKey:    nil,
			wantCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &ratelimit.StubRateLimiter{}
			s := &Server{RateLimiter: stub}

			req := httptest.NewRequest(http.MethodPost, "/verify", nil).WithContext(context.Background())

			s.applyAPIKeyRateLimits(req.Context(), req, tt.apiKey)

			if stub.UpdateCalls != tt.wantCalls {
				t.Errorf("UpdateCalls = %d, want %d", stub.UpdateCalls, tt.wantCalls)
			}

			if tt.wantCalls == 0 {
				if stub.UpdatedLeakInterval != 0 {
					t.Errorf("UpdatedLeakInterval = %v, want 0 (no update expected)", stub.UpdatedLeakInterval)
				}
				return
			}

			if uint32(stub.UpdatedCapacity) != tt.wantCapacity {
				t.Errorf("UpdatedCapacity = %d, want %d", stub.UpdatedCapacity, tt.wantCapacity)
			}
			if stub.UpdatedLeakInterval != tt.wantInterval {
				t.Errorf("UpdatedLeakInterval = %v, want %v", stub.UpdatedLeakInterval, tt.wantInterval)
			}
			if stub.UpdatedLeakInterval <= 0 {
				t.Errorf("UpdatedLeakInterval = %v, must be positive", stub.UpdatedLeakInterval)
			}
		})
	}
}

// TestVerifyDoesNotPoisonBucketOnDegenerateRPS is an end-to-end integration test
// of the fix through the real pcVerifyHandler + DB. It plants a degenerate
// requests_per_second (0) directly into backend.apikeys via an out-of-band
// UPDATE (the trigger described in the bug report), then drives the real
// pcVerifyHandler through the full middleware chain from a single source IP.
//
// The integration harness wires server.RateLimiter to a *ratelimit.StubRateLimiter
// (pkg/api/server_test.go:100) which records UpdateRequestLimits calls. Pre-fix,
// each verify called UpdateRequestLimits with leakInterval =
// time.Duration(float64(time.Second)/0) == MinInt64, so after two verifies
// stub.UpdatedLeakInterval == MinInt64 and stub.UpdateCalls == 2. Post-fix the
// guard skips the update entirely, so stub.UpdateCalls does not increase and
// stub.UpdatedLeakInterval is never written (the warn log fires instead).
//
// Requires a migrated Postgres (skipped under -short).
func TestVerifyDoesNotPoisonBucketOnDegenerateRPS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	stub, ok := server.RateLimiter.(*ratelimit.StubRateLimiter)
	if !ok {
		t.Fatalf("expected server.RateLimiter to be *ratelimit.StubRateLimiter in the test harness, got %T", server.RateLimiter)
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	// Create a key with a known burst that a poisoned update would surface via
	// stub.UpdatedCapacity, and a positive RPS the guard would otherwise accept.
	keyParams := db_tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/)
	keyParams.Scope = dbgen.ApiKeyScopePuzzle
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		t.Fatal(err)
	}
	secret := db.UUIDToSecret(apikey.ExternalID)

	// Out-of-band UPDATE: plant a degenerate requests_per_second. This mirrors
	// the operator misconfiguration the bug report describes (no in-app path
	// produces this; only a direct DB write against the unguarded column).
	if _, err := store.Pool.Exec(ctx, "UPDATE backend.apikeys SET requests_per_second = 0 WHERE id = $1", apikey.ID); err != nil {
		t.Fatalf("failed to plant degenerate RPS: %v", err)
	}

	// Force the next verify to read the planted (bad) row from DB rather than a
	// stale healthy value from the API key cache.
	cache.Delete(ctx, db.APIKeyCacheKey(secret))
	cache.Delete(ctx, db.UserAPIKeysCacheKey(user.ID))

	const fixedIP = "198.51.100.77"
	ipHeader := http.CanonicalHeaderKey(cfg.Get(common.RateLimitHeaderKey).Value())

	verifyOnce := func() *httptest.ResponseRecorder {
		puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey, property.Domain)
		if err != nil {
			t.Fatalf("solutionsSuite failed: %v", err)
		}
		payload := fmt.Sprintf("%s.%s", solutionsStr, puzzleStr)

		srv := http.NewServeMux()
		server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

		req, err := http.NewRequest(http.MethodPost, "/"+common.VerifyEndpoint, strings.NewReader(payload))
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		req.Header.Set(common.HeaderAPIKey, secret)
		req.Header.Set(common.HeaderSitekey, sitekey)
		req.Header.Set(ipHeader, fixedIP)

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	callsBefore := stub.UpdateCalls
	intervalBefore := stub.UpdatedLeakInterval

	const numVerifies = 3
	for i := 0; i < numVerifies; i++ {
		rec := verifyOnce()
		if rec.Code != http.StatusOK {
			t.Fatalf("verify #%d status = %d, want 200 (degenerate RPS must not lock out the IP; body=%q)", i+1, rec.Code, rec.Body.String())
		}
	}

	// The guard must have skipped UpdateRequestLimits for every degenerate-RPS
	// verify. Pre-fix, stub.UpdateCalls would be callsBefore+numVerifies and
	// stub.UpdatedLeakInterval would be math.MinInt64 (time.Duration(+Inf)).
	if got := stub.UpdateCalls - callsBefore; got != 0 {
		t.Errorf("stub.UpdateCalls delta = %d, want 0 (guard must not call UpdateRequestLimits for degenerate RPS)", got)
	}
	if stub.UpdatedLeakInterval != intervalBefore {
		t.Errorf("stub.UpdatedLeakInterval = %v, want %v (degenerate RPS leaked to UpdateRequestLimits)",
			stub.UpdatedLeakInterval, intervalBefore)
	}
	// And specifically: it must not be the MinInt64 sentinel the pre-fix division
	// produced on go1.27.0 linux/amd64.
	if stub.UpdatedLeakInterval == math.MinInt64 {
		t.Errorf("stub.UpdatedLeakInterval = math.MinInt64 — degenerate RPS poisoned the bucket's leakInterval")
	}
}

// TestVerifyDoesNotPoisonBucketOnDegenerateBurst is the burst-dimension
// companion to TestVerifyDoesNotPoisonBucketOnDegenerateRPS. It plants a
// degenerate requests_burst (0) directly into backend.apikeys via an
// out-of-band UPDATE (the operator-misconfiguration trigger described in the
// bug report; no in-app path produces this), then drives the real
// pcVerifyHandler through the full middleware chain from a single source IP.
//
// The planted value is 0 (not -1) because migration 000145's CHECK constraint
// now rejects negative values at the DB source (the defense-in-depth the bug
// report recommended). 0 is the value the DB still permits (CHECK is
// requests_burst >= 0) but the app guard must reject (RequestsBurst > 0): it
// would set the leaky-bucket capacity to 0 and reject every request (a per-IP
// lockout). This mirrors exactly how the RPS companion test plants
// requests_per_second = 0 (DB permits, app rejects).
//
// The negative-underflow case (requests_burst = -1 -> uint32 4294967295) is
// covered at the app layer by the "negative burst skipped" unit-test row
// (which constructs the APIKey struct directly, bypassing the DB) and at the
// DB layer by the 000145 CHECK constraint.
//
// Pre-fix, the RPS guard admitted the row (RPS=10 is valid) and the unguarded
// uint32(apiKey.RequestsBurst) cast stored uint32(0) as the bucket capacity
// (a per-IP lockout) while writing leakInterval = 100ms. Post-fix the burst
// guard (RequestsBurst > 0) skips the update entirely, so stub.UpdateCalls
// does not increase and stub.UpdatedLeakInterval is never written to 100ms
// (the warn log fires instead with requestsBurst=0).
//
// Requires a migrated Postgres (skipped under -short).
func TestVerifyDoesNotPoisonBucketOnDegenerateBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	stub, ok := server.RateLimiter.(*ratelimit.StubRateLimiter)
	if !ok {
		t.Fatalf("expected server.RateLimiter to be *ratelimit.StubRateLimiter in the test harness, got %T", server.RateLimiter)
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	// Create a key with a valid RPS (10.0); the helper sets burst = rps*5 = 50.
	// We then plant a degenerate requests_burst = 0 below, so the RPS guard
	// alone would admit the row while the burst dimension is degenerate.
	keyParams := db_tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0 /*rps*/)
	keyParams.Scope = dbgen.ApiKeyScopePuzzle
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		t.Fatal(err)
	}
	secret := db.UUIDToSecret(apikey.ExternalID)

	// Out-of-band UPDATE: plant a degenerate requests_burst (0). This mirrors
	// the operator misconfiguration the bug report describes (no in-app path
	// produces this; only a direct DB write against the column). 0 is permitted
	// by the 000145 CHECK (>= 0) but must be rejected by the app guard (> 0).
	if _, err := store.Pool.Exec(ctx, "UPDATE backend.apikeys SET requests_burst = 0 WHERE id = $1", apikey.ID); err != nil {
		t.Fatalf("failed to plant degenerate burst: %v", err)
	}

	// Force the next verify to read the planted (bad) row from DB rather than a
	// stale healthy value from the API key cache.
	cache.Delete(ctx, db.APIKeyCacheKey(secret))
	cache.Delete(ctx, db.UserAPIKeysCacheKey(user.ID))

	const fixedIP = "198.51.100.78"
	ipHeader := http.CanonicalHeaderKey(cfg.Get(common.RateLimitHeaderKey).Value())

	verifyOnce := func() *httptest.ResponseRecorder {
		puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey, property.Domain)
		if err != nil {
			t.Fatalf("solutionsSuite failed: %v", err)
		}
		payload := fmt.Sprintf("%s.%s", solutionsStr, puzzleStr)

		srv := http.NewServeMux()
		server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

		req, err := http.NewRequest(http.MethodPost, "/"+common.VerifyEndpoint, strings.NewReader(payload))
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		req.Header.Set(common.HeaderAPIKey, secret)
		req.Header.Set(common.HeaderSitekey, sitekey)
		req.Header.Set(ipHeader, fixedIP)

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	callsBefore := stub.UpdateCalls
	capacityBefore := stub.UpdatedCapacity
	intervalBefore := stub.UpdatedLeakInterval

	const numVerifies = 3
	for i := 0; i < numVerifies; i++ {
		rec := verifyOnce()
		if rec.Code != http.StatusOK {
			t.Fatalf("verify #%d status = %d, want 200 (degenerate burst must not lock out the IP; body=%q)", i+1, rec.Code, rec.Body.String())
		}
	}

	// The guard must have skipped UpdateRequestLimits for every degenerate-burst
	// verify. Pre-fix, stub.UpdateCalls would be callsBefore+numVerifies and
	// stub.UpdatedLeakInterval would be 100ms (interval from RPS=10).
	if got := stub.UpdateCalls - callsBefore; got != 0 {
		t.Errorf("stub.UpdateCalls delta = %d, want 0 (guard must not call UpdateRequestLimits for degenerate burst)", got)
	}
	if stub.UpdatedCapacity != capacityBefore {
		t.Errorf("stub.UpdatedCapacity = %d, want %d (degenerate burst leaked to UpdateRequestLimits)",
			stub.UpdatedCapacity, capacityBefore)
	}
	// Pre-fix the update would write leakInterval = 100ms (from RPS=10) along
	// with the degenerate capacity. Post-fix the guard skips entirely, so the
	// leakInterval must not change to the pre-fix value.
	if stub.UpdatedLeakInterval == 100*time.Millisecond {
		t.Errorf("stub.UpdatedLeakInterval = 100ms - degenerate burst reached UpdateRequestLimits and wrote the RPS-derived interval")
	}
	if stub.UpdatedLeakInterval != intervalBefore {
		t.Errorf("stub.UpdatedLeakInterval = %v, want %v (degenerate burst should not alter leakInterval)",
			stub.UpdatedLeakInterval, intervalBefore)
	}
}
