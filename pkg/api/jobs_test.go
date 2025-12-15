package api

import (
	"context"
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
func (j *TestJob) Name() string             { return "test_job" }
func (j *TestJob) NewParams() any           { return struct{}{} }
func (j *TestJob) Trigger() <-chan struct{} { return nil }

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

	if _, err := s.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user, time.Now().UTC().Add(-1*time.Second), t.Name()); err != nil {
		t.Fatal(err)
	}

	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Errorf("Unexpected executed flag: %v", actual)
	}
}
