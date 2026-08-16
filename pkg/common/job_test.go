package common

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type mockPeriodicJob struct {
	// We use this channel to signal the test that RunOnce was actually called
	RunSignal chan struct{}
	// We use this to trigger the job externally
	TriggerChan chan struct{}
}

func (m *mockPeriodicJob) NewParams() any           { return nil }
func (m *mockPeriodicJob) Name() string             { return "mock_periodic_job" }
func (m *mockPeriodicJob) Jitter() time.Duration    { return 1 } // Minimal jitter
func (m *mockPeriodicJob) Trigger() <-chan struct{} { return m.TriggerChan }

// Set a long interval so we know for sure the run was caused by the Trigger,
func (m *mockPeriodicJob) Interval() time.Duration { return 5 * time.Minute }
func (m *mockPeriodicJob) Timeout() time.Duration  { return 0 }

func (m *mockPeriodicJob) RunOnce(ctx context.Context, params any) error {
	// Notify the test that we ran
	m.RunSignal <- struct{}{}
	return nil
}

func TestPeriodicJobWithManualTrigger(t *testing.T) {
	ctx := t.Context()

	job := &mockPeriodicJob{
		RunSignal:   make(chan struct{}),
		TriggerChan: make(chan struct{}),
	}

	go RunPeriodicJob(ctx, job)

	// We try this twice to ensure the timer resets correctly after a manual run
	for i := 1; i <= 2; i++ {
		// 1. Send the manual trigger
		select {
		case job.TriggerChan <- struct{}{}:
			// Trigger sent successfully
		case <-time.After(1 * time.Second):
			t.Fatalf("Iteration %d: Timed out sending trigger", i)
		}

		// 2. Wait for the job to actually run
		select {
		case <-job.RunSignal:
			// Success: Job ran immediately
		case <-time.After(1 * time.Second):
			t.Fatalf("Iteration %d: Job did not run after trigger", i)
		}
	}
}

type timeoutJobStub struct {
	timedOut atomic.Bool
}

func (j *timeoutJobStub) NewParams() any { return nil }

func (j *timeoutJobStub) RunOnce(ctx context.Context, _ any) error {
	<-ctx.Done()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		j.timedOut.Store(true)
	}
	return ctx.Err()
}

func (j *timeoutJobStub) Interval() time.Duration  { return time.Hour }
func (j *timeoutJobStub) Jitter() time.Duration    { return 1 }
func (j *timeoutJobStub) Timeout() time.Duration   { return 50 * time.Millisecond }
func (j *timeoutJobStub) Name() string             { return "timeout-test-job" }
func (j *timeoutJobStub) Trigger() <-chan struct{} { return nil }

func TestRunPeriodicJobOnceRespectsTimeout(t *testing.T) {
	t.Parallel()

	job := &timeoutJobStub{}

	ctx := context.Background()

	start := time.Now()
	RunPeriodicJobOnce(ctx, job, job.NewParams(), 0)
	elapsed := time.Since(start)

	if !job.timedOut.Load() {
		t.Fatal("expected job to time out, but it did not")
	}

	if elapsed > 500*time.Millisecond {
		t.Fatalf("job took too long to return: %v", elapsed)
	}
}
