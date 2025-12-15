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
	NewParams() any
	RunOnce(ctx context.Context, params any) error
}

type PeriodicJob interface {
	NewParams() any
	RunOnce(ctx context.Context, params any) error
	// NOTE: For DB-locked Periodic job Interval() will not define the actual interval, but
	// how many times the job will _attempt_ to run. In that case the "actual" interval for
	// the most jobs will be determined by the duration of the lock they hold in DB.
	Interval() time.Duration
	// NOTE: if no jitter is needed, return 1, not 0
	Jitter() time.Duration
	Name() string
	// Return nil if manual triggering is not supported.
	Trigger() <-chan struct{}
}

type StubOneOffJob struct{}

var _ OneOffJob = (*StubOneOffJob)(nil)

func (StubOneOffJob) Name() string                       { return "StubOneOffJob" }
func (StubOneOffJob) InitialPause() time.Duration        { return 0 }
func (StubOneOffJob) NewParams() any                     { return struct{}{} }
func (StubOneOffJob) RunOnce(context.Context, any) error { return nil }

func RunOneOffJob(ctx context.Context, j OneOffJob, params any) {
	ctx = context.WithValue(ctx, TraceIDContextKey, j.Name())

	defer func() {
		if rvr := recover(); rvr != nil {
			slog.ErrorContext(ctx, "One-off job crashed", "panic", rvr, "stack", string(debug.Stack()))
		}
	}()

	time.Sleep(j.InitialPause())

	slog.DebugContext(ctx, "Running one-off job")

	if err := j.RunOnce(ctx, params); err != nil {
		slog.ErrorContext(ctx, "One-off job failed", ErrAttr(err))
	}

	slog.DebugContext(ctx, "One-off job finished")
}

// safe wrapper (with recover()) over `go f()`
func RunAdHocFunc(ctx context.Context, f func(ctx context.Context) error) {
	defer func() {
		if rvr := recover(); rvr != nil {
			slog.ErrorContext(ctx, "Ad-hoc func crashed", "panic", rvr, "stack", string(debug.Stack()))
		}
	}()

	slog.Log(ctx, LevelTrace, "Running ad-hoc func")

	if err := f(ctx); err != nil {
		slog.ErrorContext(ctx, "Ad-hoc func failed", ErrAttr(err))
	}

	slog.Log(ctx, LevelTrace, "Ad-hoc func finished")
}

func RunPeriodicJob(ctx context.Context, j PeriodicJob) {
	ctx = context.WithValue(ctx, TraceIDContextKey, j.Name())

	defer func() {
		if rvr := recover(); rvr != nil {
			slog.ErrorContext(ctx, "Periodic job crashed", "panic", rvr, "stack", string(debug.Stack()))
		}
	}()

	slog.DebugContext(ctx, "Starting periodic job")

	// If j.Trigger() returns nil, the case <-trigger below is ignored.
	trigger := j.Trigger()

	for {
		interval := j.Interval()
		jitter := j.Jitter()

		delay := interval + time.Duration(randv2.Int64N(int64(jitter)))
		timer := time.NewTimer(delay)

		var runJob bool

		select {
		case <-ctx.Done():
			_ = timer.Stop()
			slog.DebugContext(ctx, "Periodic job finished")
			return

		case <-trigger:
			// Ensure we clean up the pending timer
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			slog.DebugContext(ctx, "Forcing periodic job run", "reason", "manual_trigger")
			runJob = true

		case <-timer.C:
			slog.DebugContext(ctx, "Running periodic job once", "interval", interval.String(), "jitter", jitter.String())
			runJob = true
		}

		if runJob {
			_ = j.RunOnce(ctx, j.NewParams())
		}
	}
}

func RunPeriodicJobOnce(ctx context.Context, j PeriodicJob, params any) error {
	ctx = context.WithValue(ctx, TraceIDContextKey, j.Name())

	defer func() {
		if rvr := recover(); rvr != nil {
			slog.ErrorContext(ctx, "Periodic job crashed", "panic", rvr, "stack", string(debug.Stack()))
		}
	}()

	slog.DebugContext(ctx, "Running periodic job once")
	err := j.RunOnce(ctx, params)
	if err != nil {
		slog.ErrorContext(ctx, "Periodic job failed", ErrAttr(err))
	}
	return err
}
