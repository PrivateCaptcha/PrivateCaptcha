package leakybucket

import (
	"errors"
	"math"
	"time"
)

const (
	// although it seems to be quite generic, current implementation is somewhat locked on value "1" because
	// of how we're calculating the leaked amount in Add(). Fix is trivial (keep separate time for
	// updating the mean), but "1" is good enough anyways. Also the algorithm uses leak rate per ++second++.
	leakyBucketTimeUnitSeconds = 1
)

var (
	errPastEvent = errors.New("cannot account retroactively")
)

// we assume that one bucket will not hold more than 4*10^9 units, this also restricts max level
type TLevel = uint32

type LeakyBucket[TKey comparable] interface {
	Level(tnow time.Time) int64
	// Adds "usage" of n units. Returns how much was actually added to the bucket and previous bucket level
	Add(tnow time.Time, n TLevel) (TLevel, TLevel)
	Key() TKey
	Index() int
	SetIndex(i int)
	LastAccessTime() time.Time
	Init(key TKey, capacity TLevel, leakRatePerSecond float64, t time.Time)
}

type ConstLeakyBucket[TKey comparable] struct {
	// key of the bucket in the hashmap
	key               TKey
	lastAccessTime    time.Time
	level             TLevel
	capacity          TLevel
	leakRatePerSecond float64
	// index of this bucket in the priority queue (needed to implement it "the Go way")
	index int
}

func (lb *ConstLeakyBucket[TKey]) Init(key TKey, capacity TLevel, leakRatePerSecond float64, t time.Time) {
	lb.key = key
	lb.capacity = capacity
	lb.leakRatePerSecond = leakRatePerSecond
	lb.lastAccessTime = t
}

func (lb *ConstLeakyBucket[TKey]) LastAccessTime() time.Time {
	return lb.lastAccessTime
}

func (lb *ConstLeakyBucket[TKey]) Index() int {
	return lb.index
}

func (lb *ConstLeakyBucket[TKey]) SetIndex(i int) {
	lb.index = i
}

func (lb *ConstLeakyBucket[TKey]) Key() TKey {
	return lb.key
}

func (lb *ConstLeakyBucket[TKey]) Level(tnow time.Time) int64 {
	diff := tnow.Sub(lb.lastAccessTime)
	seconds := diff.Seconds()
	var leaked int64 = 0
	// we only leak for "future" time (from the perspective of lastAccessTime)
	if seconds > 0 {
		leaked = int64(diff.Seconds()*lb.leakRatePerSecond + 0.5)
	}
	var currLevel int64 = max(0, int64(lb.level)-leaked)
	return currLevel
}

func (lb *ConstLeakyBucket[TKey]) Add(tnow time.Time, n TLevel) (TLevel, TLevel) {
	diff := tnow.Sub(lb.lastAccessTime)
	seconds := diff.Seconds()

	var leaked int64 = 0
	// leakage is constant, so if event is in past, we already accounted for leak during that time
	// so it means that only the current level could have been larger
	if seconds > 0 {
		// there're 86400 seconds in a day so whatever the leak rate, we hope that leaked will not become so large
		// floating point number that this will impact int64 (meaning, this bucket should get GC'd faster)
		leaked = int64(diff.Seconds()*lb.leakRatePerSecond + 0.5)
		lb.lastAccessTime = tnow
	}

	prevLevel := lb.level
	var currLevel int64 = max(0, int64(lb.level)-leaked)
	var nextLevel int64 = min(int64(lb.capacity), currLevel+int64(n))
	lb.level = TLevel(nextLevel)

	return prevLevel, TLevel(nextLevel - currLevel)
}

func NewConstBucket[TKey comparable](key TKey, capacity TLevel, leakRatePerSecond float64, t time.Time) *ConstLeakyBucket[TKey] {
	b := &ConstLeakyBucket[TKey]{}
	b.Init(key, capacity, leakRatePerSecond, t)
	return b
}

// Variable LeakyBucket, that updates it's leakRatePerSecond with a leakyBucketTimeUnitSeconds step
type VarLeakyBucket[TKey comparable] struct {
	ConstLeakyBucket[TKey]
	// ------ non-leak-specific fields ------
	// running mean of all the elements added to the bucket. LeakyBucket adapts leak rate to
	// changing statistical properties of the elements added to the bucket
	mean float64
	// we change {mean} only in different time windows (current resolution is 1 second)
	// and {pendingSum} is what accumulates added elements for the last time window
	pendingSum int64
	// total count of items added to the bucket. NOTE: in case of uint64 overflow happens
	// we just reset all stats and continue as usual
	count uint64
}

func NewVarBucket[TKey comparable](key TKey, capacity TLevel, leakRatePerSecond float64, t time.Time) *VarLeakyBucket[TKey] {
	b := &VarLeakyBucket[TKey]{}
	b.Init(key, capacity, leakRatePerSecond, t)
	return b
}

func (lb *VarLeakyBucket[TKey]) Add(tnow time.Time, n TLevel) (TLevel, TLevel) {
	tnow = tnow.Truncate(leakyBucketTimeUnitSeconds * time.Second)

	diff := tnow.Sub(lb.lastAccessTime)
	seconds := diff.Seconds()

	var leaked int64 = 0
	if seconds > 0 {
		leaked = int64(seconds*lb.leakRatePerSecond + 0.5)
	}

	prevLevel := lb.level
	var currLevel int64 = max(0, int64(lb.level)-leaked)
	var nextLevel int64 = min(int64(lb.capacity), currLevel+int64(n))
	lb.level = TLevel(nextLevel)

	lb.pendingSum += int64(n)

	if pendingCount := math.Abs(seconds); pendingCount >= leakyBucketTimeUnitSeconds {
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
		if seconds > 0 {
			lb.lastAccessTime = tnow
		}
	}

	return prevLevel, TLevel(nextLevel - currLevel)
}
