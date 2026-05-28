package api

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/rs/xid"
)

type TestJob struct {
	count int32
}

var _ common.PeriodicJob = (*TestJob)(nil)

func (j *TestJob) RunOnce(ctx context.Context, params any) error {
	atomic.AddInt32(&j.count, 1)
	return nil
}
func (j *TestJob) Interval() time.Duration  { return 200 * time.Millisecond }
func (j *TestJob) Jitter() time.Duration    { return 1 }
func (j *TestJob) Timeout() time.Duration   { return 0 }
func (j *TestJob) Name() string             { return "test_job" }
func (j *TestJob) NewParams() any           { return struct{}{} }
func (j *TestJob) Trigger() <-chan struct{} { return nil }

type stubJobWithError struct {
	executed      int32
	shouldFail    bool
	errToReturn   error
	executionTime time.Duration
	name          string
}

var _ common.PeriodicJob = (*stubJobWithError)(nil)

func newStubJobWithError() *stubJobWithError {
	return &stubJobWithError{
		name: "stubJobWithError_" + xid.New().String(),
	}
}

func (j *stubJobWithError) Name() string {
	return j.name
}

func (j *stubJobWithError) Trigger() <-chan struct{} {
	return nil
}

func (j *stubJobWithError) Interval() time.Duration {
	return 10 * time.Millisecond
}

func (j *stubJobWithError) Timeout() time.Duration {
	return 1 * time.Minute
}

func (j *stubJobWithError) Jitter() time.Duration {
	return 1
}

func (j *stubJobWithError) NewParams() any {
	return struct{}{}
}

func (j *stubJobWithError) RunOnce(ctx context.Context, params any) error {
	atomic.StoreInt32(&j.executed, 1)

	if j.executionTime > 0 {
		time.Sleep(j.executionTime)
	}

	if j.shouldFail {
		return j.errToReturn
	}

	return nil
}

func (j *stubJobWithError) wasExecuted() bool {
	return atomic.LoadInt32(&j.executed) == 1
}

func TestUniqueJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	job := &TestJob{}

	uniqueJob := &maintenance.UniquePeriodicJob{
		Job:          job,
		Store:        store,
		LockDuration: 1 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := common.RunPeriodicJobOnce(ctx, uniqueJob, uniqueJob.NewParams()); err != nil {
		t.Fatal(err)
	}
	cancel()

	if job.count == 0 || job.count > 3 {
		t.Fatalf("Unexpected count of job executions: %v", job.count)
	}
}

func TestAsyncJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	var executed int32 = 0
	job := maintenance.NewAsyncTasksJob(store)
	handlerID := xid.New().String()
	job.Register(handlerID, func(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
		atomic.AddInt32(&executed, 1)
		return nil, nil
	})
	defer job.Deregister(handlerID)

	ctx := t.Context()

	request := struct{}{}

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := server.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user, time.Now().UTC().Add(-1*time.Second), t.Name()); err != nil {
		t.Fatal(err)
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Errorf("Unexpected executed flag: %v", actual)
	}
}

func TestUniqueJobReleasesLockOnError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	expectedError := errors.New("inner job failed")
	innerJob := newStubJobWithError()
	innerJob.shouldFail = true
	innerJob.errToReturn = expectedError

	job := &maintenance.UniquePeriodicJob{
		Job:          innerJob,
		Store:        store,
		LockDuration: 10 * time.Minute,
	}

	// First run - should acquire lock, run job (which fails), then release lock
	err := job.RunOnce(ctx, job.NewParams())
	if !errors.Is(err, expectedError) {
		t.Errorf("Expected error %v, got: %v", expectedError, err)
	}

	if !innerJob.wasExecuted() {
		t.Error("Inner job should have been executed even though it failed")
	}

	// Verify lock was released by trying to acquire it again immediately
	lock, err := store.Impl().RetrieveLock(ctx, innerJob.Name())
	if err == nil && lock.ExpiresAt.Valid && lock.ExpiresAt.Time.After(time.Now().UTC()) {
		t.Error("Lock should have been released after job failure")
	}

	// Second run should be able to acquire the lock and run
	innerJob2 := newStubJobWithError()
	job2 := &maintenance.UniquePeriodicJob{
		Job:          innerJob2,
		Store:        store,
		LockDuration: 10 * time.Minute,
	}

	err = job2.RunOnce(ctx, job2.NewParams())
	if err != nil {
		t.Errorf("Second run should succeed, got error: %v", err)
	}

	if !innerJob2.wasExecuted() {
		t.Error("Second inner job should have been executed after lock was released")
	}
}

func TestUniqueJobLockPreventsExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Use a unique job name to avoid conflicts with other tests
	jobName := "test_lock_prevents_" + xid.New().String()

	// Create inner job with longer execution time
	innerJob1 := newStubJobWithError()
	innerJob1.executionTime = 100 * time.Millisecond
	innerJob1.name = jobName

	job1 := &maintenance.UniquePeriodicJob{
		Job:          innerJob1,
		Store:        store,
		LockDuration: 5 * time.Minute,
	}

	// Start first job in background
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- job1.RunOnce(ctx, job1.NewParams())
	}()

	// Give some time for first job to acquire lock
	time.Sleep(20 * time.Millisecond)

	// Second job should fail to acquire lock
	innerJob2 := newStubJobWithError()
	innerJob2.name = jobName

	job2 := &maintenance.UniquePeriodicJob{
		Job:          innerJob2,
		Store:        store,
		LockDuration: 5 * time.Minute,
	}

	// This should not execute because lock is held
	err2 := job2.RunOnce(ctx, job2.NewParams())
	if err2 != nil {
		t.Logf("Second job returned expected error: %v", err2)
	}

	// Wait for first job to complete
	<-doneCh

	if !innerJob1.wasExecuted() {
		t.Error("First inner job should have been executed")
	}

	if innerJob2.wasExecuted() {
		t.Error("Second inner job should NOT have been executed due to lock")
	}
}

func TestWarmupAPICacheJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create a user and org to have API keys
	plan := db_tests.CreateNewSubscriptionParams(testPlan)
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
	records := []*common.VerifyRecord{
		{
			PropertyID: property.ID,
			UserID:     user.ID,
			OrgID:      org.ID,
			Timestamp:  time.Now().UTC(),
		},
	}
	timeSeries.WriteVerifyLogBatch(ctx, records)

	// Clear cache for the API key before running the job
	apiSecret := db.UUIDToSecret(apiKey.ExternalID)
	cache.Delete(ctx, db.APIKeyCacheKey(apiSecret))

	// Run the warmup job
	job := &maintenance.WarmupAPICacheJob{
		Store:      store,
		TimeSeries: timeSeries,
		Backoff:    1 * time.Millisecond,
		Limit:      100,
	}

	for attempt := 0; attempt < 4; attempt++ {
		// we now enabled async writes in ClickHouse so flush takes some time
		time.Sleep(200 * time.Millisecond)

		err = job.RunOnce(ctx, job.NewParams())
		if err != nil {
			t.Fatalf("WarmupAPICacheJob failed: %v", err)
		}

		// Verify that API key is now cached
		if _, err := store.Impl().GetCachedAPIKey(ctx, apiSecret); err == nil {
			break
		}
	}

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

func TestDeactivateFailingFormsJobEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())
	registerTemplatesJob := &maintenance.RegisterEmailTemplatesJob{
		Templates: email.Templates(),
		Store:     store,
	}
	if err := registerTemplatesJob.RunOnce(ctx, registerTemplatesJob.NewParams()); err != nil {
		t.Fatalf("failed to register email templates: %v", err)
	}

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	failingForm, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "deactivate-failing.example.com"),
		&dbgen.CreateFormParams{
			Name:              t.Name() + " failing",
			URL:               "https://example.com/failing",
			Fields:            []byte(`{}`),
			Enabled:           true,
			RequestsPerSecond: 1,
			RequestsBurst:     5,
			RetryRequestCount: 0,
			Method:            dbgen.FormMethodPost,
		}, org)
	if err != nil {
		t.Fatalf("failed to create failing form: %v", err)
	}

	healthyForm, _, _, err := store.Impl().CreateNewForm(ctx, db_tests.CreateNewPropertyParams(user.ID, "deactivate-healthy.example.com"), &dbgen.CreateFormParams{
		Name:              t.Name() + " healthy",
		URL:               "https://example.com/healthy",
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		t.Fatalf("failed to create healthy form: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Hour)
	if err := timeSeries.WriteFormSubmitBatch(ctx, []*common.FormSubmitRecord{
		{UserID: user.ID, OrgID: org.ID, FormID: failingForm.ID, Timestamp: now.Add(-4 * time.Hour), Status: 0},
		{UserID: user.ID, OrgID: org.ID, FormID: failingForm.ID, Timestamp: now.Add(-3 * time.Hour), Status: 1},
		{UserID: user.ID, OrgID: org.ID, FormID: failingForm.ID, Timestamp: now.Add(-2 * time.Hour), Status: 1},
		{UserID: user.ID, OrgID: org.ID, FormID: failingForm.ID, Timestamp: now.Add(-1 * time.Hour), Status: 1},
		{UserID: user.ID, OrgID: org.ID, FormID: healthyForm.ID, Timestamp: now.Add(-3 * time.Hour), Status: 1},
		{UserID: user.ID, OrgID: org.ID, FormID: healthyForm.ID, Timestamp: now.Add(-2 * time.Hour), Status: 0},
		{UserID: user.ID, OrgID: org.ID, FormID: healthyForm.ID, Timestamp: now.Add(-1 * time.Hour), Status: 1},
	}); err != nil {
		t.Fatalf("failed to write form submit batch: %v", err)
	}

	foundCandidate := false
	for attempt := 0; attempt < 10; attempt++ {
		candidates, err := timeSeries.RetrieveFailingForms(ctx, 3, 10)
		if err != nil {
			t.Fatalf("failed to retrieve failing forms: %v", err)
		}
		for _, candidate := range candidates {
			if candidate != nil && candidate.FormID == failingForm.ID {
				foundCandidate = true
				break
			}
		}
		if foundCandidate {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !foundCandidate {
		t.Fatalf("failing form %d was not detected by RetrieveFailingForms within retry window", failingForm.ID)
	}

	job := &maintenance.DeactivateFailingFormsJob{
		Store:      store,
		TimeSeries: timeSeries,
		PortalURL:  "https://portal.example",
		IDHasher:   server.IDHasher,
		Threshold:  3,
		MaxForms:   10,
	}
	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatalf("job failed: %v", err)
	}

	querier := dbgen.New(store.Pool)
	deactivatedForm, err := querier.GetFormByID(ctx, failingForm.ID)
	if err != nil {
		t.Fatalf("failed to retrieve failing form: %v", err)
	}
	if deactivatedForm.Active {
		t.Fatalf("failing form should be inactive after job")
	}

	stillActiveForm, err := querier.GetFormByID(ctx, healthyForm.ID)
	if err != nil {
		t.Fatalf("failed to retrieve healthy form: %v", err)
	}
	if !stillActiveForm.Active {
		t.Fatalf("healthy form should remain active")
	}

	notifications, err := store.Impl().RetrievePendingUserNotifications(ctx, time.Now().UTC().Add(-1*time.Minute), 100, 5)
	if err != nil {
		t.Fatalf("failed to retrieve pending notifications: %v", err)
	}

	var matched *dbgen.GetPendingUserNotificationsRow
	for _, notification := range notifications {
		if notification.UserNotification.UserID.Valid &&
			notification.UserNotification.UserID.Int32 == user.ID &&
			notification.UserNotification.TemplateID.Valid &&
			notification.UserNotification.TemplateID.String == email.FormDeactivationTemplate.Hash() {
			matched = notification
			break
		}
	}
	if matched == nil {
		t.Fatalf("expected form deactivation notification for user %d", user.ID)
	}

	var payload email.FormDeactivationContext
	if err := json.Unmarshal(matched.UserNotification.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal notification payload: %v", err)
	}
	if len(payload.Forms) != 1 {
		t.Fatalf("payload form count = %d, want 1", len(payload.Forms))
	}
	if payload.Forms[0].Name != failingForm.Name {
		t.Fatalf("payload form name = %q, want %q", payload.Forms[0].Name, failingForm.Name)
	}
	expectedLink := "https://portal.example/org/" + server.IDHasher.Encrypt(int(org.ID)) + "/form/" + server.IDHasher.Encrypt(int(failingForm.ID))
	if payload.Forms[0].Link != expectedLink {
		t.Fatalf("payload form link = %q, want %q", payload.Forms[0].Link, expectedLink)
	}
}
