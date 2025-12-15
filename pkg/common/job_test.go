package common

import (
	"context"
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
