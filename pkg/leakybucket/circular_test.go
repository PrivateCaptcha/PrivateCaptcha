package leakybucket

import (
	"math"
	"testing"
	"time"
)

func TestCircularBucketAccumulatesCurrentInterval(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	bucket := NewCircularBucket(interval, t0)

	bucket.Add(t0, 2, 1)
	bucket.Add(t0.Add(interval-time.Nanosecond), 3, 4)

	current, previous := bucket.Counts(t0.Add(interval - time.Nanosecond))
	if current != (CircularBucketCounters{Puzzles: 5, Verifications: 5}) {
		t.Errorf("Current counters = %+v, want 5 puzzles and 5 verifications", current)
	}
	if previous != (CircularBucketCounters{}) {
		t.Errorf("Previous counters = %+v, want empty counters", previous)
	}
}

func TestCircularBucketRotatesIntervals(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	bucket := NewCircularBucket(interval, t0)
	bucket.Add(t0, 2, 1)

	bucket.Add(t0.Add(interval), 3, 4)
	current, previous := bucket.Counts(t0.Add(interval))
	if current != (CircularBucketCounters{Puzzles: 3, Verifications: 4}) {
		t.Errorf("Current counters after rotation = %+v, want 3 puzzles and 4 verifications", current)
	}
	if previous != (CircularBucketCounters{Puzzles: 2, Verifications: 1}) {
		t.Errorf("Previous counters after rotation = %+v, want 2 puzzles and 1 verification", previous)
	}

	bucket.Add(t0.Add(2*interval), 5, 6)
	current, previous = bucket.Counts(t0.Add(2 * interval))
	if current != (CircularBucketCounters{Puzzles: 5, Verifications: 6}) {
		t.Errorf("Current counters after second rotation = %+v, want 5 puzzles and 6 verifications", current)
	}
	if previous != (CircularBucketCounters{Puzzles: 3, Verifications: 4}) {
		t.Errorf("Previous counters after second rotation = %+v, want 3 puzzles and 4 verifications", previous)
	}
}

func TestCircularBucketRotatesEquivalentTimesFromDifferentLocations(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	bucket := NewCircularBucket(interval, t0)
	bucket.Add(t0, 2, 1)

	nextInterval := t0.Add(interval).In(time.FixedZone("alternate UTC", 0))
	current, previous := bucket.Counts(nextInterval)
	if current != (CircularBucketCounters{}) {
		t.Errorf("Current counters = %+v, want empty counters", current)
	}
	if previous != (CircularBucketCounters{Puzzles: 2, Verifications: 1}) {
		t.Errorf("Previous counters = %+v, want retained counters", previous)
	}
}

func TestCircularBucketClearsExpiredIntervals(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	bucket := NewCircularBucket(interval, t0)
	bucket.Add(t0, 2, 1)

	current, previous := bucket.Counts(t0.Add(2 * interval))
	if current != (CircularBucketCounters{}) || previous != (CircularBucketCounters{}) {
		t.Errorf("Counters after two inactive intervals = current %+v, previous %+v; want both empty", current, previous)
	}
}

func TestCircularBucketRetainsLatePreviousIntervalEvents(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	bucket := NewCircularBucket(interval, t0)
	bucket.Add(t0.Add(interval), 2, 1)

	bucket.Add(t0.Add(interval-time.Second), 3, 4)
	current, previous := bucket.Counts(t0.Add(interval))
	if current != (CircularBucketCounters{Puzzles: 2, Verifications: 1}) {
		t.Errorf("Current counters = %+v, want 2 puzzles and 1 verification", current)
	}
	if previous != (CircularBucketCounters{Puzzles: 3, Verifications: 4}) {
		t.Errorf("Previous counters = %+v, want late event counters", previous)
	}
}

func TestCircularBucketDiscardsEventsOlderThanPreviousInterval(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	bucket := NewCircularBucket(interval, t0)
	bucket.Add(t0.Add(2*interval), 2, 1)

	bucket.Add(t0, 3, 4)
	current, previous := bucket.Counts(t0.Add(2 * interval))
	if current != (CircularBucketCounters{Puzzles: 2, Verifications: 1}) {
		t.Errorf("Current counters = %+v, want 2 puzzles and 1 verification", current)
	}
	if previous != (CircularBucketCounters{}) {
		t.Errorf("Previous counters = %+v, want old event discarded", previous)
	}
}

func TestCircularBucketCountersSaturate(t *testing.T) {
	interval := 5 * time.Minute
	t0 := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	bucket := NewCircularBucket(interval, t0)

	bucket.Add(t0, math.MaxUint32, math.MaxUint32)
	bucket.Add(t0, 1, 1)

	current, _ := bucket.Counts(t0)
	if current.Puzzles != math.MaxUint32 || current.Verifications != math.MaxUint32 {
		t.Errorf("Counters wrapped after overflow: %+v", current)
	}
}
