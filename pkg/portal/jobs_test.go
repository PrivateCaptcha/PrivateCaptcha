package portal

import (
	"context"
	"errors"
	"testing"
	"time"

	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type registrationFinalizerStore struct {
	session.Store
	errors         []error
	retrySucceeded bool
	calls          int
	onCall         func()
}

func (s *registrationFinalizerStore) FinalizeRegistration(context.Context, *session.Session, int32) (bool, error) {
	s.calls++
	if s.onCall != nil {
		s.onCall()
	}
	if len(s.errors) > 0 {
		err := s.errors[0]
		s.errors = s.errors[1:]
		return false, err
	}
	s.retrySucceeded = true
	return true, nil
}

func TestRegistrationFinalizerRetriesTransientFailure(t *testing.T) {
	store := &registrationFinalizerStore{
		errors: []error{errors.New("temporary database failure")},
	}
	sess := session.NewSession(session.NewSessionData("processing-sid"), store)
	job := &registrationFinalizerJob{Sess: sess, UserID: 42}

	if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
		t.Fatal(err)
	}
	if !store.retrySucceeded {
		t.Fatal("registration finalization did not succeed after the transient failure")
	}
}

func TestRegistrationFinalizerReturnsLastFailure(t *testing.T) {
	errs := []error{
		errors.New("database unavailable 1"),
		errors.New("database unavailable 2"),
		errors.New("database unavailable 3"),
		errors.New("database unavailable 4"),
		errors.New("database unavailable 5"),
	}
	lastErr := errs[len(errs)-1]
	store := &registrationFinalizerStore{
		errors: errs,
	}
	sess := session.NewSession(session.NewSessionData("processing-sid"), store)
	job := &registrationFinalizerJob{Sess: sess, UserID: 42}

	if err := job.RunOnce(t.Context(), job.NewParams()); !errors.Is(err, lastErr) {
		t.Fatalf("RunOnce() error = %v, want %v", err, lastErr)
	}
	if store.calls != registrationFinalizeMaxAttempts {
		t.Fatalf("FinalizeRegistration() calls = %d, want %d", store.calls, registrationFinalizeMaxAttempts)
	}
}

func TestRegistrationFinalizerStopsDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store := &registrationFinalizerStore{
		errors: []error{errors.New("temporary database failure")},
		onCall: cancel,
	}
	sess := session.NewSession(session.NewSessionData("processing-sid"), store)
	job := &registrationFinalizerJob{Sess: sess, UserID: 42}

	if err := job.RunOnce(ctx, job.NewParams()); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error = %v, want %v", err, context.Canceled)
	}
	if store.calls != 1 {
		t.Fatalf("FinalizeRegistration() calls = %d, want 1", store.calls)
	}
}

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

func TestCleanupDeletedRecordsJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create the cleanup job
	job := &maintenance.CleanupDeletedRecordsJob{
		Store: store,
		Age:   30 * 24 * time.Hour, // 30 days
	}

	// Run the job - it should not fail even with no deleted records
	err := job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Errorf("CleanupDeletedRecordsJob.RunOnce() error = %v", err)
	}
}

func TestCleanupDeletedRecordsJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &maintenance.CleanupDeletedRecordsJob{
		Store: store,
		Age:   30 * 24 * time.Hour, // Default to 30 days
	}

	// Run with invalid params (wrong type) - should use default
	err := job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupDeletedRecordsJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}

func TestCleanupUserNotificationsJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create the cleanup job
	job := &maintenance.CleanupUserNotificationsJob{
		Store:              store,
		NotificationMonths: 1, // Cleanup notifications older than 1 month
		TemplateMonths:     6,
	}

	// Run the job - it should not fail even with no notifications to clean
	err := job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Errorf("CleanupUserNotificationsJob.RunOnce() error = %v", err)
	}
}

func TestCleanupUserNotificationsJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &maintenance.CleanupUserNotificationsJob{
		Store:              store,
		NotificationMonths: 3,
		TemplateMonths:     6,
	}

	// Run with invalid params (wrong type) - should use default
	err := job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupUserNotificationsJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}
