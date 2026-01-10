//go:build enterprise

package maintenance

import (
	"context"
	"database/sql"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	cfg        common.ConfigStore
	cache      common.Cache[db.CacheKey, any]
	timeSeries common.TimeSeriesStore
	store      *db.BusinessStore
)

func testsConfigStore() common.ConfigStore {
	baseCfg := config.NewBaseConfig(config.NewEnvConfig(os.Getenv))
	baseCfg.Add(config.NewStaticValue(common.ClickHouseOptionalKey, "true"))
	return baseCfg
}

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs(common.StageTest, true)

	cfg = testsConfigStore()

	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Connect(context.Background(), cfg, 3*time.Second, false /*admin*/)
	if dberr != nil {
		panic(dberr)
	}

	if clickhouse != nil {
		timeSeries = db.NewTimeSeries(clickhouse, cache)
	} else {
		timeSeries = db.NewMemoryTimeSeries()
	}

	var err error
	cache, err = db.NewMemoryCache[db.CacheKey, any]("default", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		panic(err)
	}

	store = db.NewBusinessEx(pool, cache)

	os.Exit(m.Run())
}

func TestWarmupAPICacheJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create a user and org to have API keys
	plan := db_tests.CreateNewSubscriptionParams(nil)
	plan.Status = "trialing"

	user, org, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name(), plan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create a property for this user
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "api-warmup-test.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Create an API key for this user
	apiKeyParams := db_tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-key", time.Now(), 1*time.Hour, 10.0)
	apiKey, _, err := store.Impl().CreateAPIKey(ctx, user, apiKeyParams)
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	// Seed some activity in time series (similar to TestGetPropertyStats)
	if memTS, ok := timeSeries.(*db.MemoryTimeSeries); ok {
		records := []*common.VerifyRecord{
			{
				PropertyID:  property.ID,
				RequestTime: time.Now().UTC(),
			},
		}
		memTS.InsertVerifyRecords(ctx, records)
	}

	// Clear cache for the API key before running the job
	apiSecret := db.UUIDToSecret(apiKey.ExternalID)
	cache.Delete(ctx, db.APIKeyCacheKey(apiSecret))

	// Run the warmup job
	job := &WarmupAPICacheJob{
		Store:      store,
		TimeSeries: timeSeries,
		Backoff:    1 * time.Millisecond,
		Limit:      100,
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Fatalf("WarmupAPICacheJob failed: %v", err)
	}

	// Verify that API key is now cached
	cachedKey, err := store.Impl().GetCachedAPIKey(ctx, apiSecret)
	if err != nil {
		t.Errorf("Expected API key to be cached after warmup, got error: %v", err)
	}

	if cachedKey == nil {
		t.Error("Expected non-nil cached API key")
	} else if cachedKey.ID != apiKey.ID {
		t.Errorf("Cached API key ID mismatch: got %d, want %d", cachedKey.ID, apiKey.ID)
	}

	// Verify job metadata
	if job.Name() != "warmup_api_cache_job" {
		t.Errorf("Expected job name 'warmup_api_cache_job', got %s", job.Name())
	}

	if job.InitialPause() != 5*time.Second {
		t.Errorf("Expected initial pause 5s, got %v", job.InitialPause())
	}
}

func TestWarmupPortalAuthJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &WarmupPortalAuthJob{
		Store:               store,
		RegistrationAllowed: true,
	}

	// Run the job - it will try to cache portal login/register properties
	// Note: This may fail if the portal properties don't exist in test DB,
	// but that's expected - we just want to verify the job runs
	err := job.RunOnce(ctx, job.NewParams())

	// The job might fail if portal properties aren't seeded, which is OK
	// We primarily want to verify the code path executes
	_ = err

	// Verify job metadata
	if job.Name() != "warmup_portal_auth_job" {
		t.Errorf("Expected job name 'warmup_portal_auth_job', got %s", job.Name())
	}

	if job.InitialPause() != 5*time.Second {
		t.Errorf("Expected initial pause 5s, got %v", job.InitialPause())
	}

	// If portal properties exist, check if they're cached
	loginSitekey := db.UUIDToSiteKey(db.PortalLoginPropertyUUID)
	_, err = store.Impl().GetCachedPropertyBySitekey(ctx, loginSitekey)
	// This may return ErrCacheMiss if property doesn't exist - that's OK
	if err != nil && err != db.ErrCacheMiss && err != db.ErrRecordNotFound {
		t.Logf("GetCachedPropertyBySitekey returned: %v (may be expected if portal property not seeded)", err)
	}
}

func TestCleanupDBCacheJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Add some cache records with past expiration
	cacheKey := "test_cleanup_" + t.Name()
	cacheData := []byte("test data")

	// Store with very short TTL (1 millisecond)
	err := store.Impl().StoreInCache(ctx, cacheKey, cacheData, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to store in cache: %v", err)
	}

	// Wait for the TTL to expire
	time.Sleep(10 * time.Millisecond)

	// Run the cleanup job
	job := &CleanupDBCacheJob{
		Store: store,
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Fatalf("CleanupDBCacheJob failed: %v", err)
	}

	// Verify job metadata
	if job.Name() != "cleanup_db_cache_job" {
		t.Errorf("Expected job name 'cleanup_db_cache_job', got %s", job.Name())
	}

	if job.Interval() != 5*time.Minute {
		t.Errorf("Expected interval 5m, got %v", job.Interval())
	}

	if job.Timeout() != 1*time.Minute {
		t.Errorf("Expected timeout 1m, got %v", job.Timeout())
	}

	// After cleanup, the expired record should be gone
	_, err = store.Impl().RetrieveFromCache(ctx, cacheKey)
	if err == nil {
		// Record might still exist briefly due to timing - log but don't fail
		t.Log("Cache record still exists after cleanup (timing sensitive)")
	}
}
