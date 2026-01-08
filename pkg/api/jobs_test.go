package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
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
}

var _ common.PeriodicJob = (*stubJobWithError)(nil)

func (j *stubJobWithError) Name() string {
	return "stubJobWithError_" + xid.New().String()
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

	user, _, err := db_test.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
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
	innerJob := &stubJobWithError{
		shouldFail:  true,
		errToReturn: expectedError,
	}
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
	innerJob2 := &stubJobWithError{}
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
	innerJob1 := &stubJobWithError{
		executionTime: 100 * time.Millisecond,
	}
	// Override the Name method for this test
	job1 := &maintenance.UniquePeriodicJob{
		Job:          &namedJob{stubJobWithError: innerJob1, name: jobName},
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
	innerJob2 := &stubJobWithError{}
	job2 := &maintenance.UniquePeriodicJob{
		Job:          &namedJob{stubJobWithError: innerJob2, name: jobName},
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

// namedJob wraps stubJobWithError with a custom name
type namedJob struct {
	*stubJobWithError
	name string
}

func (j *namedJob) Name() string {
	return j.name
}
