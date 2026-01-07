package maintenance

import (
	"os"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func dataTestConfig() common.ConfigStore {
	baseCfg := config.NewBaseConfig(config.NewEnvConfig(os.Getenv))
	baseCfg.Add(config.NewStaticValue(common.ClickHouseOptionalKey, "true"))
	return baseCfg
}

func TestCleanupAuditLogJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, dataTestConfig(), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	querier := dbgen.New(pool)
	store := db.NewBusinessEx(pool, cache)

	// Create a test user for audit logs
	user, err := querier.CreateUser(ctx, &dbgen.CreateUserParams{
		Name:  "Test Audit User",
		Email: "auditlogtest@privatecaptcha.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = querier.SoftDeleteUser(ctx, user.ID)
	})

	// Create old audit logs using direct SQL insert
	oldTime := time.Now().UTC().Add(-30 * 24 * time.Hour) // 30 days ago
	_, err = querier.CreateAuditLogs(ctx, []*dbgen.CreateAuditLogsParams{
		{
			UserID:      db.Int(user.ID),
			Action:      dbgen.AuditLogActionAccess,
			Source:      dbgen.AuditLogSourcePortal,
			EntityID:    db.Int8(1),
			EntityTable: "test",
			SessionID:   "test-session-1",
			CreatedAt:   db.Timestampz(oldTime),
		},
		{
			UserID:      db.Int(user.ID),
			Action:      dbgen.AuditLogActionAccess,
			Source:      dbgen.AuditLogSourcePortal,
			EntityID:    db.Int8(2),
			EntityTable: "test",
			SessionID:   "test-session-2",
			CreatedAt:   db.Timestampz(oldTime.Add(-1 * time.Hour)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create the cleanup job with immediate cleanup (0 past interval)
	job := &CleanupAuditLogJob{
		BusinessDB:   store,
		PastInterval: 0,
	}

	// Run the job
	err = job.RunOnce(ctx, &CleanupAuditLogParams{
		PastInterval: 0, // Cleanup everything before now
	})
	if err != nil {
		t.Errorf("CleanupAuditLogJob.RunOnce() error = %v", err)
	}

	// Verify job methods
	if job.Name() != "cleanup_audit_log_job" {
		t.Errorf("Expected job name 'cleanup_audit_log_job', got '%s'", job.Name())
	}

	if job.Interval() != 1*time.Hour {
		t.Errorf("Expected interval 1h, got %v", job.Interval())
	}

	if job.Timeout() != 1*time.Minute {
		t.Errorf("Expected timeout 1m, got %v", job.Timeout())
	}

	if job.Trigger() != nil {
		t.Error("Expected nil trigger")
	}
}

func TestCleanupAsyncTasksJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, dataTestConfig(), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	// Create old async tasks
	oldTime := time.Now().UTC().Add(-30 * 24 * time.Hour) // 30 days ago
	_, err = store.Impl().CreateNewAsyncTask(ctx, map[string]string{"key": "value"}, "test_handler", nil, oldTime, "test-ref-1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Impl().CreateNewAsyncTask(ctx, map[string]string{"key": "value2"}, "test_handler", nil, oldTime.Add(-1*time.Hour), "test-ref-2")
	if err != nil {
		t.Fatal(err)
	}

	// Create the cleanup job
	job := &CleanupAsyncTasksJob{
		BusinessDB:   store,
		PastInterval: 0,
	}

	// Run the job - it will cleanup tasks older than now
	err = job.RunOnce(ctx, &CleanupAsyncTasksParams{
		PastInterval: 0, // Cleanup everything before now
	})
	if err != nil {
		t.Errorf("CleanupAsyncTasksJob.RunOnce() error = %v", err)
	}

	// Verify job methods
	if job.Name() != "cleanup_async_tasks_job" {
		t.Errorf("Expected job name 'cleanup_async_tasks_job', got '%s'", job.Name())
	}

	if job.Interval() != 3*time.Hour {
		t.Errorf("Expected interval 3h, got %v", job.Interval())
	}

	if job.Timeout() != 1*time.Minute {
		t.Errorf("Expected timeout 1m, got %v", job.Timeout())
	}

	if job.Trigger() != nil {
		t.Error("Expected nil trigger")
	}
}

func TestCleanupAuditLogJobNewParams(t *testing.T) {
	job := &CleanupAuditLogJob{
		PastInterval: 7 * 24 * time.Hour,
	}

	params := job.NewParams()
	p, ok := params.(*CleanupAuditLogParams)
	if !ok {
		t.Fatal("NewParams() did not return *CleanupAuditLogParams")
	}

	if p.PastInterval != 7*24*time.Hour {
		t.Errorf("Expected PastInterval 7d, got %v", p.PastInterval)
	}
}

func TestCleanupAsyncTasksJobNewParams(t *testing.T) {
	job := &CleanupAsyncTasksJob{
		PastInterval: 14 * 24 * time.Hour,
	}

	params := job.NewParams()
	p, ok := params.(*CleanupAsyncTasksParams)
	if !ok {
		t.Fatal("NewParams() did not return *CleanupAsyncTasksParams")
	}

	if p.PastInterval != 14*24*time.Hour {
		t.Errorf("Expected PastInterval 14d, got %v", p.PastInterval)
	}
}

func TestCleanupAuditLogJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, dataTestConfig(), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	job := &CleanupAuditLogJob{
		BusinessDB:   store,
		PastInterval: 30 * 24 * time.Hour, // Default to 30 days
	}

	// Run with invalid params (wrong type) - should use default
	err = job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupAuditLogJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}

func TestCleanupAsyncTasksJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, dataTestConfig(), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	job := &CleanupAsyncTasksJob{
		BusinessDB:   store,
		PastInterval: 30 * 24 * time.Hour, // Default to 30 days
	}

	// Run with invalid params (wrong type) - should use default
	err = job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupAsyncTasksJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}
