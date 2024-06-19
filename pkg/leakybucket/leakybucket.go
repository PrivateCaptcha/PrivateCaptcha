package leakybucket

import (
	"errors"
	"time"

	"golang.org/x/exp/constraints"
)

const (
	// although it seems to be quite generic, current implementation is somewhat locked on value "1" because
	// of how we're calculating the leaked amount in Add(). Fix is trivial (keep separate time for
	// updating the mean), but "1" is good enough anyways
	leakyBucketTimeUnitSeconds = 1
)

var (
	errPastEvent = errors.New("cannot account retroactively")
)

// this is one of the main core pieces of logic for difficuly scaling
type LeakyBucket[TKey constraints.Ordered] struct {
	// key of the bucket in the hashmap
	key               TKey
	lastAccessTime    time.Time
	level             int64
	capacity          int64
	leakRatePerSecond float64
	// ------ non-leak-specific fields ------
	// running mean of all the elements added to the bucket. LeakyBucket adapts to changing statistical properties
	// of the elements added to the bucket
	mean float64
	// we change {mean} only in different time windows (current resolution is 1 second)
	// and {pendingSum} is what accumulates added elements for the last time window
	pendingSum int64
	// total count of items added to the bucket. NOTE: in case of uint64 overflow happens
	// we just reset all stats and continue as usual
	count uint64
	// index of this bucket in the priority queue (needed to implement it "the Go way")
	index int
}

func NewBucket[TKey constraints.Ordered](key TKey, capacity int64, leakRatePerSecond float64, t time.Time) *LeakyBucket[TKey] {
	return &LeakyBucket[TKey]{
		key:               key,
		lastAccessTime:    t.Truncate(leakyBucketTimeUnitSeconds * time.Second),
		level:             0,
		capacity:          capacity,
		mean:              0,
		pendingSum:        0,
		count:             0,
		index:             -1,
		leakRatePerSecond: leakRatePerSecond,
	}
}

// bucket's Level, in a sense, is the sum of deviations from the mean
// just the subtraction of mean is "delayed"
func (lb *LeakyBucket[TKey]) Level(tnow time.Time) int64 {
	diff := tnow.Sub(lb.lastAccessTime)
	leaked := int64(diff.Seconds()*lb.leakRatePerSecond + 0.5)
	level := max(0.0, lb.level-leaked)
	return level
}

// Backfill preserves the leak rate as we don't know the "final" leak rate yet
func (lb *LeakyBucket[TKey]) Backfill(tnow time.Time, n int64) int64 {
	leakRate := lb.leakRatePerSecond
	result := lb.doAdd(tnow, n)
	lb.leakRatePerSecond = leakRate
	return result
}

func (lb *LeakyBucket[TKey]) Add(tnow time.Time, n int64) (int64, error) {
	if tnow.Before(lb.lastAccessTime) {
		return 0, errPastEvent
	}

	return lb.doAdd(tnow, n), nil
}

func (lb *LeakyBucket[TKey]) doAdd(tnow time.Time, n int64) int64 {
	tnow = tnow.Truncate(leakyBucketTimeUnitSeconds * time.Second)

	diff := tnow.Sub(lb.lastAccessTime)
	seconds := diff.Seconds()

	leaked := int64(seconds*lb.leakRatePerSecond + 0.5)
	currLevel := max(0, lb.level-leaked)
	nextLevel := min(lb.capacity, currLevel+n)
	lb.level = nextLevel

	lb.pendingSum += n

	if seconds >= leakyBucketTimeUnitSeconds {
		pendingCount := seconds
		lb.count++

		// unlikely uint64 "overflow" protection
		if lb.count == 0 {
			lb.count = 1
			lb.mean = 0.0
		}

		// we multiply mean by pendingCount accordingly to the formula of calculating mean if we skipped some elements
		// M[k] = (x[k] + x[k-1] + ... + x[1]) / k
		// M[k] = (x[k] + (k-1)*M[k-1]) / k     (k*M[k] just gives the sum of k elements)
		// or, if we will skip x[k-1] and x[k-2] elements (they are 0)
		// M[k] = (x[k] + x[k-1] + x[k-2] + (k-3)*M[k-3]) / k (and so on)
		// elements for us are zeroes for "missing" time windows without a value
		lb.mean = lb.mean + (float64(lb.pendingSum)-pendingCount*lb.mean)/(float64(lb.count)+pendingCount)
		lb.pendingSum = 0
		lb.leakRatePerSecond = lb.mean
		lb.lastAccessTime = tnow
	}

	return nextLevel - currLevel
}
