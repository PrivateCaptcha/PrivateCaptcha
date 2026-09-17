package leakybucket

import (
	"time"

	"github.com/maypok86/otter/v2"
)

const (
	maxCircularBuckets             = 100_000
	minimumVerificationRateSamples = 10
)

type CircularBucketManager struct {
	buckets  *otter.Cache[int32, *CircularBucket]
	interval time.Duration
}

func NewCircularBucketManager(interval time.Duration) *CircularBucketManager {
	return newCircularBucketManager(interval, maxCircularBuckets, nil)
}

func newCircularBucketManager(interval time.Duration, maxBuckets int, clock otter.Clock) *CircularBucketManager {
	return &CircularBucketManager{
		buckets: otter.Must(&otter.Options[int32, *CircularBucket]{
			MaximumSize:      maxBuckets,
			InitialCapacity:  max(100, maxBuckets/1000),
			ExpiryCalculator: otter.ExpiryAccessing[int32, *CircularBucket](interval),
			Clock:            clock,
		}),
		interval: interval,
	}
}

func (m *CircularBucketManager) RecordPuzzles(propertyID int32, count uint32, tnow time.Time) {
	m.record(propertyID, count, 0, tnow)
}

func (m *CircularBucketManager) RecordVerifications(propertyID int32, count uint32, tnow time.Time) {
	m.record(propertyID, 0, count, tnow)
}

func (m *CircularBucketManager) record(propertyID int32, puzzles, verifications uint32, tnow time.Time) {
	_, _ = m.buckets.Compute(propertyID, func(bucket *CircularBucket, found bool) (*CircularBucket, otter.ComputeOp) {
		if !found {
			bucket = NewCircularBucket(m.interval, tnow)
		}
		bucket.Add(tnow, puzzles, verifications)
		return bucket, otter.WriteOp
	})
}

func (m *CircularBucketManager) VerificationRate(propertyID int32, tnow time.Time) float64 {
	rate := 1.0
	_, _ = m.buckets.Compute(propertyID, func(bucket *CircularBucket, found bool) (*CircularBucket, otter.ComputeOp) {
		if !found {
			return nil, otter.CancelOp
		}

		current, previous := bucket.Counts(tnow)
		puzzles := uint64(current.Puzzles) + uint64(previous.Puzzles)
		verifications := uint64(current.Verifications) + uint64(previous.Verifications)
		if puzzles+verifications >= minimumVerificationRateSamples {
			rate = float64(puzzles) / float64(verifications)
		}
		return bucket, otter.WriteOp
	})
	return rate
}
