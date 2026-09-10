package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type puzzleStatsFinalizerStub struct {
	processed        []bool
	calls            int
	before           []time.Time
	onCall           func()
	monthlyProcessed bool
	monthlyCalls     int
	monthlyBefore    []time.Time
}

func (s *puzzleStatsFinalizerStub) FinalizeNextPuzzleStats(ctx context.Context, before time.Time) (bool, error) {
	s.before = append(s.before, before)
	processed := s.processed[s.calls]
	s.calls++
	if s.onCall != nil {
		s.onCall()
	}
	return processed, nil
}

func (s *puzzleStatsFinalizerStub) FinalizeNextPuzzleStatsMonth(ctx context.Context, before time.Time) (bool, error) {
	s.monthlyBefore = append(s.monthlyBefore, before)
	s.monthlyCalls++
	return s.monthlyProcessed, nil
}

func TestPuzzleStatsFinalizationJobRunOnceLimitsPartitions(t *testing.T) {
	now := time.Date(2026, time.June, 1, 0, 0, 30, 0, time.UTC)
	wantBefore := now.Add(-2 * time.Hour)
	finalizer := &puzzleStatsFinalizerStub{processed: []bool{true, true, true}}
	job := &PuzzleStatsFinalizationJob{
		Finalizer:     finalizer,
		MaxPartitions: 2,
		Now:           func() time.Time { return now },
	}

	if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
		t.Fatal(err)
	}
	if finalizer.calls != 2 {
		t.Fatalf("finalizer calls = %d, want 2", finalizer.calls)
	}
	for _, before := range finalizer.before {
		if !before.Equal(wantBefore) {
			t.Fatalf("finalization cutoff = %v, want %v", before, wantBefore)
		}
	}
	if finalizer.monthlyCalls != 0 {
		t.Fatalf("monthly finalizer calls = %d, want 0 while daily partitions remain", finalizer.monthlyCalls)
	}

	drainedFinalizer := &puzzleStatsFinalizerStub{processed: []bool{false}, monthlyProcessed: true}
	drainedJob := &PuzzleStatsFinalizationJob{
		Finalizer:     drainedFinalizer,
		MaxPartitions: 2,
		Now:           func() time.Time { return now },
	}
	if err := drainedJob.RunOnce(t.Context(), drainedJob.NewParams()); err != nil {
		t.Fatal(err)
	}
	if drainedFinalizer.monthlyCalls != 1 {
		t.Fatalf("monthly finalizer calls = %d, want 1", drainedFinalizer.monthlyCalls)
	}
	if !drainedFinalizer.monthlyBefore[0].Equal(wantBefore) {
		t.Fatalf("monthly finalization cutoff = %v, want %v", drainedFinalizer.monthlyBefore[0], wantBefore)
	}
}

func TestPuzzleStatsFinalizationJobStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finalizer := &puzzleStatsFinalizerStub{processed: []bool{true, true}, onCall: cancel}
	job := &PuzzleStatsFinalizationJob{Finalizer: finalizer, MaxPartitions: 2}

	err := job.RunOnce(ctx, job.NewParams())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error = %v, want context cancellation", err)
	}
	if finalizer.calls != 1 {
		t.Fatalf("finalizer calls = %d, want 1", finalizer.calls)
	}
	if finalizer.monthlyCalls != 0 {
		t.Fatalf("monthly finalizer calls = %d, want 0", finalizer.monthlyCalls)
	}
}
