//go:build enterprise

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/jackc/pgx/v5/pgtype"
)

// newExpiredTrialSubscriptionParams builds an internal "trialing" subscription whose
// trial_ends_at lies in the past and whose next_billed_at is NULL. This is exactly the
// row shape that ExpireInternalTrials' UpdateInternalSubscriptions query matches, so
// calling ExpireInternalTrials will flip the subscription status to pc-trial-expired.
func newExpiredTrialSubscriptionParams() *dbgen.CreateSubscriptionParams {
	params := db_tests.CreateNewSubscriptionParams(testPlan)
	params.TrialEndsAt = db.Timestampz(time.Now().UTC().AddDate(0, 0, -7))
	params.NextBilledAt = pgtype.Timestamptz{} // NULL - required by UpdateInternalSubscriptions WHERE clause
	return params
}

// expireTrialNow mutates the user's internal trial subscription from pc-trial-active to
// pc-trial-expired in the database, using the same code path as the hourly
// ExpireInternalTrialsJob. The window is generous enough to always contain the
// subscription's trial_ends_at (now-7d).
func expireTrialNow(ctx context.Context, t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30)
	to := now.AddDate(0, 0, -1)
	if err := store.Impl().ExpireInternalTrials(ctx, from, to, billing.InternalStatusTrialing, billing.InternalStatusExpired); err != nil {
		t.Fatalf("Failed to expire internal trials: %v", err)
	}
}

// flagUserAsViolator installs a positive userLimits entry for the user by invoking the
// public UserLimiter.CheckUsers API, which runs RetrieveUsersWithoutSubscription. For a
// pc-trial-expired user this installs a positive (violator) entry, after which
// EvaluateAPIAccess reports (false, nil) and both the cached and postponed API key
// paths are expected to reject the caller.
func flagUserAsViolator(ctx context.Context, t *testing.T, userID int32) {
	t.Helper()
	if err := server.Auth.Limiter.CheckUsers(ctx, map[int32]uint{userID: 1}); err != nil {
		t.Fatalf("Failed to check users for subscription violations: %v", err)
	}

	allowed, err := server.Auth.Limiter.EvaluateAPIAccess(ctx, userID)
	if err != nil {
		t.Fatalf("Expected EvaluateAPIAccess to report a positive userLimits entry (err=nil), got err=%v", err)
	}
	if allowed {
		t.Fatalf("Expected EvaluateAPIAccess to return allowed=false for a flagged violator, got true")
	}
}

// evictAPIKeyCache forces the next request to take the postponed DB-lookup path inside
// apiKeyOwnerSource.apiKey, mirroring the pattern already used by verify/jobs tests.
func evictAPIKeyCache(ctx context.Context, t *testing.T, secret string, userID int32) {
	t.Helper()
	cache.Delete(ctx, db.APIKeyCacheKey(secret))
	cache.Delete(ctx, db.UserAPIKeysCacheKey(userID))
}

// TestAPIOrgExpiredTrialUserWithUncachedKeyIsBlocked exercises the portal "management"
// API path on a per-process API key cache miss. A pc-trial-expired user who has been
// flagged in the userLimits cache must be rejected with HTTP 403 even when their API key
// is not cached, because apiKeyOwnerSource.OwnerID must run the same EvaluateAPIAccess
// subscription check on the postponed DB-lookup path that the APIKey middleware runs on
// the cached branch. This is the regression that the bug report attributes to the missing
// Auth field on the portal-path apiKeyOwnerSource construction.
func TestAPIOrgExpiredTrialUserWithUncachedKeyIsBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name(), newExpiredTrialSubscriptionParams())
	if err != nil {
		t.Fatal(err)
	}

	keyParams := db_tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0)
	keyParams.Scope = dbgen.ApiKeyScopePortal
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		t.Fatal(err)
	}
	apiKeyStr := db.UUIDToSecret(apikey.ExternalID)

	expireTrialNow(ctx, t)
	flagUserAsViolator(ctx, t, user.ID)
	evictAPIKeyCache(ctx, t, apiKeyStr, user.ID)

	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.OrganizationsEndpoint, apiKeyStr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("BUG: expected HTTP 403 for flagged expired-trial user on the portal key-cache-miss path, got 200 (bypass succeeds)")
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 for flagged expired-trial user on key-cache-miss path, got %d", resp.StatusCode)
	}
}

// TestAPIOrgTrialingUserWithUncachedKeySucceeds is a non-regression guard: a user with an
// active (non-expired) trial subscription must still reach the portal handler and receive
// HTTP 200 on a key-cache miss. This ensures that wiring Auth onto the portal
// apiKeyOwnerSource did not introduce over-blocking for legitimate subscribers on the
// postponed DB-lookup path.
func TestAPIOrgTrialingUserWithUncachedKeySucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	// Default trial subscription: status pc-trial-active, trial_ends_at in the future.
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	keyParams := db_tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0)
	keyParams.Scope = dbgen.ApiKeyScopePortal
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		t.Fatal(err)
	}
	apiKeyStr := db.UUIDToSecret(apikey.ExternalID)

	// The user has no userLimits entry (no traffic-driven backfill in this fixture), so
	// EvaluateAPIAccess reports ErrCacheMiss and the postponed path must not block.
	evictAPIKeyCache(ctx, t, apiKeyStr, user.ID)

	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.OrganizationsEndpoint, apiKeyStr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for trialing user on key-cache-miss path, got %d", resp.StatusCode)
	}
}
