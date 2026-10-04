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
