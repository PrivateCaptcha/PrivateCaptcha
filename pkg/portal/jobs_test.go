package portal

import (
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func TestCleanupAuditLogJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create a test user using the helper function
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Impl().SoftDeleteUser(ctx, user)
	})

	// Create the cleanup job with immediate cleanup (0 past interval)
	job := &maintenance.CleanupAuditLogJob{
		BusinessDB:   store,
		PastInterval: 0,
	}

	// Run the job
	err = job.RunOnce(ctx, &maintenance.CleanupAuditLogParams{
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

	// Create old async tasks
	oldTime := time.Now().UTC().Add(-30 * 24 * time.Hour) // 30 days ago
	_, err := store.Impl().CreateNewAsyncTask(ctx, map[string]string{"key": "value"}, "test_handler", nil, oldTime, "test-ref-1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Impl().CreateNewAsyncTask(ctx, map[string]string{"key": "value2"}, "test_handler", nil, oldTime.Add(-1*time.Hour), "test-ref-2")
	if err != nil {
		t.Fatal(err)
	}

	// Create the cleanup job
	job := &maintenance.CleanupAsyncTasksJob{
		BusinessDB:   store,
		PastInterval: 0,
	}

	// Run the job - it will cleanup tasks older than now
	err = job.RunOnce(ctx, &maintenance.CleanupAsyncTasksParams{
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
	job := &maintenance.CleanupAuditLogJob{
		PastInterval: 7 * 24 * time.Hour,
	}

	params := job.NewParams()
	p, ok := params.(*maintenance.CleanupAuditLogParams)
	if !ok {
		t.Fatal("NewParams() did not return *CleanupAuditLogParams")
	}

	if p.PastInterval != 7*24*time.Hour {
		t.Errorf("Expected PastInterval 7d, got %v", p.PastInterval)
	}
}

func TestCleanupAsyncTasksJobNewParams(t *testing.T) {
	job := &maintenance.CleanupAsyncTasksJob{
		PastInterval: 14 * 24 * time.Hour,
	}

	params := job.NewParams()
	p, ok := params.(*maintenance.CleanupAsyncTasksParams)
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

	job := &maintenance.CleanupAuditLogJob{
		BusinessDB:   store,
		PastInterval: 30 * 24 * time.Hour, // Default to 30 days
	}

	// Run with invalid params (wrong type) - should use default
	err := job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupAuditLogJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}

func TestCleanupAsyncTasksJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &maintenance.CleanupAsyncTasksJob{
		BusinessDB:   store,
		PastInterval: 30 * 24 * time.Hour, // Default to 30 days
	}

	// Run with invalid params (wrong type) - should use default
	err := job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupAsyncTasksJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}

// Placeholder to satisfy the compiler for the unused import
var _ = db.ErrRecordNotFound
