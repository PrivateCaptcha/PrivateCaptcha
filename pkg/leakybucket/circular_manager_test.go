package leakybucket

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircularBucketManagerCalculatesRateAcrossRetainedIntervals(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	manager := NewCircularBucketManager(interval)

	manager.RecordPuzzles(42, 6, t0)
	manager.RecordVerifications(42, 2, t0)
	manager.RecordPuzzles(42, 3, t0.Add(interval))
	manager.RecordVerifications(42, 1, t0.Add(interval))

	if rate := manager.VerificationRate(42, t0.Add(interval)); rate != 3.0 {
		t.Errorf("Verification rate = %v, want 3", rate)
	}
}

func TestCircularBucketManagerReturnsNeutralRateWithoutData(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	manager := NewCircularBucketManager(interval)

	if rate := manager.VerificationRate(42, t0); rate != 1.0 {
		t.Errorf("Verification rate without a bucket = %v, want 1", rate)
	}

	manager.RecordPuzzles(42, 2, t0)
	if rate := manager.VerificationRate(42, t0.Add(2*interval)); rate != 1.0 {
		t.Errorf("Verification rate without retained counters = %v, want 1", rate)
	}
}

func TestCircularBucketManagerDefersSparseOneSidedRates(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	manager := NewCircularBucketManager(interval)

	manager.RecordPuzzles(1, 1, t0)
	if rate := manager.VerificationRate(1, t0); rate != 1.0 {
		t.Errorf("Sparse puzzle-only verification rate = %v, want neutral rate", rate)
	}
	manager.RecordPuzzles(1, 9, t0)
	if rate := manager.VerificationRate(1, t0); !math.IsInf(rate, 1) {
		t.Errorf("Sampled puzzle-only verification rate = %v, want +Inf", rate)
	}

	manager.RecordVerifications(2, 10, t0)
	if rate := manager.VerificationRate(2, t0); rate != 0.0 {
		t.Errorf("Verification-only rate = %v, want 0", rate)
	}
}

func TestCircularBucketManagerRecordsConcurrently(t *testing.T) {
	const count = 1_000
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	manager := NewCircularBucketManager(interval)

	var wg sync.WaitGroup
	for range count {
		wg.Add(2)
		go func() {
			defer wg.Done()
			manager.RecordPuzzles(42, 1, t0)
		}()
		go func() {
			defer wg.Done()
			manager.RecordVerifications(42, 1, t0)
		}()
	}
	wg.Wait()

	bucket, found := manager.buckets.GetIfPresent(42)
	if !found {
		t.Fatal("Property bucket was not found")
	}
	current, previous := bucket.Counts(t0)
	if current != (CircularBucketCounters{Puzzles: count, Verifications: count}) {
		t.Errorf("Concurrent counters = %+v, want %d of each", current, count)
	}
	if previous != (CircularBucketCounters{}) {
		t.Errorf("Previous counters = %+v, want empty counters", previous)
	}
}

func TestCircularBucketManagerExpiresAfterLastAccess(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	clock := newCircularManagerClock()
	manager := newCircularBucketManager(interval, 8, clock)
	manager.RecordPuzzles(42, minimumVerificationRateSamples, t0)

	clock.Advance(interval - time.Second)
	if rate := manager.VerificationRate(42, t0); !math.IsInf(rate, 1) {
		t.Fatalf("Rate before expiry = %v, want +Inf", rate)
	}

	clock.Advance(interval - time.Second)
	if rate := manager.VerificationRate(42, t0); !math.IsInf(rate, 1) {
		t.Fatalf("Rate before extended expiry = %v, want +Inf", rate)
	}

	clock.Advance(interval + time.Second)
	if rate := manager.VerificationRate(42, t0); rate != 1.0 {
		t.Errorf("Rate after expiry = %v, want neutral rate", rate)
	}
}

type circularManagerClock struct {
	now atomic.Int64
}

func newCircularManagerClock() *circularManagerClock {
	clock := &circularManagerClock{}
	clock.now.Store(1)
	return clock
}

func (c *circularManagerClock) NowNano() int64 {
	return c.now.Load()
}

func (c *circularManagerClock) Tick(time.Duration) <-chan time.Time {
	return make(chan time.Time)
}

func (c *circularManagerClock) Advance(duration time.Duration) {
	c.now.Add(duration.Nanoseconds())
}
