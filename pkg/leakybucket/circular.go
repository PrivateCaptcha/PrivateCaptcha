package leakybucket

import (
	"math"
	"time"
)

type CircularBucketCounters struct {
	Puzzles       uint32
	Verifications uint32
}

type CircularBucket struct {
	interval     time.Duration
	currentStart time.Time
	current      CircularBucketCounters
	previous     CircularBucketCounters
}

func NewCircularBucket(interval time.Duration, tnow time.Time) *CircularBucket {
	if interval <= 0 {
		panic("circular bucket interval must be positive")
	}

	return &CircularBucket{
		interval:     interval,
		currentStart: tnow.Truncate(interval),
	}
}

func (b *CircularBucket) Add(tnow time.Time, puzzles, verifications uint32) {
	windowStart := tnow.Truncate(b.interval)
	b.advance(windowStart)

	counters := &b.current
	if windowStart.Before(b.currentStart) {
		if !windowStart.Equal(b.currentStart.Add(-b.interval)) {
			return
		}
		counters = &b.previous
	}

	counters.Puzzles = saturatingAdd(counters.Puzzles, puzzles)
	counters.Verifications = saturatingAdd(counters.Verifications, verifications)
}

func (b *CircularBucket) Counts(tnow time.Time) (CircularBucketCounters, CircularBucketCounters) {
	b.advance(tnow.Truncate(b.interval))
	return b.current, b.previous
}

func (b *CircularBucket) advance(windowStart time.Time) {
	if !windowStart.After(b.currentStart) {
		return
	}

	if windowStart.Equal(b.currentStart.Add(b.interval)) {
		b.previous = b.current
	} else {
		b.previous = CircularBucketCounters{}
	}
	b.current = CircularBucketCounters{}
	b.currentStart = windowStart
}

func saturatingAdd(a, b uint32) uint32 {
	if b > math.MaxUint32-a {
		return math.MaxUint32
	}
	return a + b
}
