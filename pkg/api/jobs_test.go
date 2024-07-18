package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

type TestJob struct {
	count int32
}

func (j *TestJob) RunOnce(ctx context.Context) error {
	atomic.AddInt32(&j.count, 1)
	return nil
}
func (j *TestJob) Interval() time.Duration { return 200 * time.Millisecond }
func (j *TestJob) Jitter() time.Duration   { return 1 }
func (j *TestJob) Name() string            { return "test_job" }

func TestUniqueJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	job := &TestJob{}

	uniqueJob := &maintenance.UniquePeriodicJob{
		Job:          job,
		Store:        store,
		LockDuration: 1 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go common.RunPeriodicJob(ctx, uniqueJob)

	time.Sleep(3 * time.Second)
	cancel()

	if job.count == 0 || job.count > 3 {
		t.Fatalf("Unexpected count of job executions: %v", job.count)
	}
}
