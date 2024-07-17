package common

import (
	"context"
	"log/slog"
	randv2 "math/rand/v2"
	"runtime/debug"
	"time"
)

type PeriodicJob interface {
	RunOnce(ctx context.Context) error
	Interval() time.Duration
	Jitter() time.Duration
	Name() string
}

func RunPeriodicJob(ctx context.Context, j PeriodicJob) {
	jlog := slog.With("name", j.Name())

	defer func() {
		if rvr := recover(); rvr != nil {
			jlog.ErrorContext(ctx, "Periodic job crashed", "panic", rvr, "stack", string(debug.Stack()))
		}
	}()

	jlog.DebugContext(ctx, "Running periodic job", "interval", j.Interval().String())

	interval := j.Interval()
	jitter := j.Jitter()

	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
			// introduction of jitter is supposed to help in case we have multiple workers to distribute the load
		case <-time.After(interval + time.Duration(randv2.Int64N(int64(jitter)))):
			jlog.DebugContext(ctx, "Running periodic job once")
			_ = j.RunOnce(ctx)
		}
	}

	jlog.DebugContext(ctx, "Peridic job finished")
}
