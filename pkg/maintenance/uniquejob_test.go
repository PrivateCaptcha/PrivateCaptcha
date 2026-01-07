package maintenance

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type stubJobWithError struct {
	executed      int32
	shouldFail    bool
	errToReturn   error
	executionTime time.Duration
}

var _ common.PeriodicJob = (*stubJobWithError)(nil)

func (j *stubJobWithError) Name() string {
	return "stubJobWithError"
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

func uniqueJobTestConfig() common.ConfigStore {
	baseCfg := config.NewBaseConfig(config.NewEnvConfig(os.Getenv))
	baseCfg.Add(config.NewStaticValue(common.ClickHouseOptionalKey, "true"))
	return baseCfg
}

func TestUniqueJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, uniqueJobTestConfig(), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	innerJob := &stubJobWithError{}
	job := &UniquePeriodicJob{
		Job:          innerJob,
		Store:        store,
		LockDuration: 1 * time.Minute,
	}

	err = job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !innerJob.wasExecuted() {
		t.Error("Inner job should have been executed")
	}
}

func TestUniqueJobReleasesLockOnError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, uniqueJobTestConfig(), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	expectedError := errors.New("inner job failed")
	innerJob := &stubJobWithError{
		shouldFail:  true,
		errToReturn: expectedError,
	}
	job := &UniquePeriodicJob{
		Job:          innerJob,
		Store:        store,
		LockDuration: 10 * time.Minute, // Long duration - should be released on error
	}

	// First run - should acquire lock, run job (which fails), then release lock
	err = job.RunOnce(ctx, job.NewParams())
	if err != expectedError {
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
	job2 := &UniquePeriodicJob{
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

	cache, err := db.NewMemoryCache[db.CacheKey, any]("test", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	pool, _, err := db.Connect(ctx, uniqueJobTestConfig(), 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}

	store := db.NewBusinessEx(pool, cache)

	// Create inner job with longer execution time
	innerJob1 := &stubJobWithError{
		executionTime: 100 * time.Millisecond,
	}
	job1 := &UniquePeriodicJob{
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
	innerJob2 := &stubJobWithError{}
	job2 := &UniquePeriodicJob{
		Job:          innerJob2,
		Store:        store,
		LockDuration: 5 * time.Minute,
	}

	// This should not execute because lock is held - the error is expected (lock not acquired)
	// We don't check the error because the lock acquisition failure is an expected case
	err2 := job2.RunOnce(ctx, job2.NewParams())
	if err2 != nil {
		// Expected - lock could not be acquired
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
