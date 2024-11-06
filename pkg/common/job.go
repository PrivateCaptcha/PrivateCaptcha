package common

import (
	"context"
	"log/slog"
	randv2 "math/rand/v2"
	"runtime/debug"
	"time"
)

type OneOffJob interface {
	Name() string
	InitialPause() time.Duration
	RunOnce(ctx context.Context) error
}

type PeriodicJob interface {
	RunOnce(ctx context.Context) error
	// NOTE: For DB-locked Periodic job Interval() will not define the actual interval, but
	// how many times the job will _attempt_ to run. In that case the "actual" interval for
	// the most jobs will be determined by the duration of the lock they hold in DB.
	Interval() time.Duration
	// NOTE: if no jitter is needed, return 1, not 0
	Jitter() time.Duration
	Name() string
}

func RunOneOffJob(ctx context.Context, j OneOffJob) {
	jlog := slog.With("name", j.Name())

	defer func() {
		if rvr := recover(); rvr != nil {
			jlog.ErrorContext(ctx, "Periodic job crashed", "panic", rvr, "stack", string(debug.Stack()))
		}
	}()

	time.Sleep(j.InitialPause())

	jlog.DebugContext(ctx, "Running one-off job")

	j.RunOnce(ctx)

	jlog.DebugContext(ctx, "One-off job finished")
}

func RunPeriodicJob(ctx context.Context, j PeriodicJob) {
	jlog := slog.With("name", j.Name())

	defer func() {
		if rvr := recover(); rvr != nil {
			jlog.ErrorContext(ctx, "Periodic job crashed", "panic", rvr, "stack", string(debug.Stack()))
		}
	}()

	jlog.DebugContext(ctx, "Starting periodic job", "interval", j.Interval().String())

	interval := j.Interval()
	jitter := j.Jitter()

	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
			// introduction of jitter is supposed to help in case we have multiple workers to distribute the load
		case <-time.After(interval + time.Duration(randv2.Int64N(int64(jitter)))):
			jlog.Log(ctx, LevelTrace, "Running periodic job once")
			_ = j.RunOnce(ctx)
		}
	}

	jlog.DebugContext(ctx, "Periodic job finished")
}
