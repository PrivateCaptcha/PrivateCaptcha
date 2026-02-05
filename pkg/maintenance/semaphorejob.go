package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"golang.org/x/sync/semaphore"
)

type semaphorePeriodicJob struct {
	job common.PeriodicJob
	sem *semaphore.Weighted
}

var _ common.PeriodicJob = (*semaphorePeriodicJob)(nil)

func (j *semaphorePeriodicJob) Interval() time.Duration  { return j.job.Interval() }
func (j *semaphorePeriodicJob) Jitter() time.Duration    { return j.job.Jitter() }
func (j *semaphorePeriodicJob) Name() string             { return j.job.Name() }
func (j *semaphorePeriodicJob) NewParams() any           { return j.job.NewParams() }
func (j *semaphorePeriodicJob) Trigger() <-chan struct{} { return j.job.Trigger() }
func (j *semaphorePeriodicJob) Timeout() time.Duration   { return j.job.Timeout() }

func (j *semaphorePeriodicJob) RunOnce(ctx context.Context, params any) error {
	slog.DebugContext(ctx, "About to acquire maintenance job semaphore", "job", j.Name())

	if err := j.sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer j.sem.Release(1)

	return j.job.RunOnce(ctx, params)
}

type semaphoreOneOffJob struct {
	job common.OneOffJob
	sem *semaphore.Weighted
}

var _ common.OneOffJob = (*semaphoreOneOffJob)(nil)

func (j *semaphoreOneOffJob) Name() string                { return j.job.Name() }
func (j *semaphoreOneOffJob) InitialPause() time.Duration { return j.job.InitialPause() }
func (j *semaphoreOneOffJob) NewParams() any              { return j.job.NewParams() }

func (j *semaphoreOneOffJob) RunOnce(ctx context.Context, params any) error {
	slog.DebugContext(ctx, "About to acquire maintenance job semaphore", "job", j.Name())

	if err := j.sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer j.sem.Release(1)

	return j.job.RunOnce(ctx, params)
}
