package leakybucket

import (
	"time"
)

// we assume that one bucket will not hold more than 4*10^9 units, this also restricts max level
type TLevel = uint32

type LeakyBucket[TKey comparable] interface {
	Level(tnow time.Time) TLevel
	// Adds "usage" of n units. Returns how much was actually added to the bucket and previous bucket level
	Add(tnow time.Time, n TLevel) (TLevel, TLevel)
	Update(capacity TLevel, leakInterval time.Duration)
	Key() TKey
	Index() int
	SetIndex(i int)
	LastAccessTime() time.Time
	LeakInterval() time.Duration
	Capacity() TLevel
	Init(key TKey, capacity TLevel, leakInterval time.Duration, t time.Time)
}

type LimitUpdaterFunc func(capacity TLevel, leakInterval time.Duration)

type ConstLeakyBucket[TKey comparable] struct {
	// key of the bucket in the hashmap
	key            TKey
	lastAccessTime time.Time
	level          TLevel
	capacity       TLevel
	// each {leakInterval} we loose 1 bucket level
	// e.g. to have 5 levels/second leak rate use {leakInterval = time.Second / 5}
	leakInterval time.Duration
	// index of this bucket in the priority queue (needed to implement it "the Go way")
	index int
}

func (lb *ConstLeakyBucket[TKey]) Init(key TKey, capacity TLevel, leakInterval time.Duration, tnow time.Time) {
	lb.key = key
	lb.capacity = capacity
	lb.leakInterval = leakInterval
	lb.lastAccessTime = tnow
}

func (lb *ConstLeakyBucket[TKey]) Index() int {
	return lb.index
}

func (lb *ConstLeakyBucket[TKey]) SetIndex(i int) {
	lb.index = i
}

func (lb *ConstLeakyBucket[TKey]) LeakInterval() time.Duration {
	return lb.leakInterval
}

func (lb *ConstLeakyBucket[TKey]) Capacity() TLevel {
	return lb.capacity
}

func (lb *ConstLeakyBucket[TKey]) Key() TKey {
	return lb.key
}

func (lb *ConstLeakyBucket[TKey]) LastAccessTime() time.Time {
	return lb.lastAccessTime
}

func (lb *ConstLeakyBucket[TKey]) Update(capacity TLevel, leakInterval time.Duration) {
	lb.capacity = capacity
	lb.leakInterval = leakInterval
}

func (lb *ConstLeakyBucket[TKey]) Level(tnow time.Time) TLevel {
	diff := tnow.Sub(lb.lastAccessTime)
	var leaked = int64(diff / lb.leakInterval)
	var currLevel = max(0, int64(lb.level)-leaked)
	return TLevel(currLevel)
}

func (lb *ConstLeakyBucket[TKey]) Add(tnow time.Time, n TLevel) (TLevel, TLevel) {
	diff := tnow.Sub(lb.lastAccessTime)

	var leaked = max(int64(diff/lb.leakInterval), 0)
	// leakage is constant, so if event is in past, we already accounted for leak during that time
	// so it means that only the current level could have been larger
	if diff > 0 {
		// We took leekage into account at {leakRate} boundary so this is preserving the "unaccounted" part of a leak
		lb.lastAccessTime = tnow.Truncate(lb.leakInterval)
	}

	var currLevel = max(0, int64(lb.level)-leaked)
	var nextLevel = min(int64(lb.capacity), currLevel+int64(n))
	lb.level = TLevel(nextLevel)

	// {current level}, {how much added}
	return TLevel(nextLevel), TLevel(nextLevel - currLevel)
}

func NewConstBucket[TKey comparable](key TKey, capacity TLevel, leakInterval time.Duration, t time.Time) *ConstLeakyBucket[TKey] {
	b := &ConstLeakyBucket[TKey]{}
	b.Init(key, capacity, leakInterval, t)
	return b
}

// Variable LeakyBucket, that updates it's leaking rate
type VarLeakyBucket[TKey comparable] struct {
	ConstLeakyBucket[TKey]
	// ConstLeakBucket always leaks 1 level per {leakInterval}, but {VarLeakyBucket} removes {leakRate} levels
	// and reculates {leakRate} after each {leakInterval} (leakRate is the running mean)
	leakRate float64
	// we change {leakRate} only in different time windows (with resolution of {leakInterval})
	// and {pendingSum} is what accumulates added elements for the yet unaccounted time window
	pendingSum int64
	// total count of items added to the bucket. NOTE: in case of uint64 overflow happens
	// we just reset all stats and continue as usual
	count uint64
}

func NewVarBucket[TKey comparable](key TKey, capacity TLevel, leakInterval time.Duration, t time.Time) *VarLeakyBucket[TKey] {
	b := &VarLeakyBucket[TKey]{
		leakRate:   1.0, // to start like the ConstLeakyBucket, we leak 1 level per leakInterval
		pendingSum: 0,
		count:      1, // this is needed to have "previous" empty bucket for averages
	}
	b.Init(key, capacity, leakInterval, t)
	return b
}

func (lb *VarLeakyBucket[TKey]) LeakInterval() time.Duration {
	nanoseconds := float64(lb.leakInterval.Nanoseconds()) / lb.leakRate
	return time.Duration(nanoseconds) * time.Nanosecond
}

func (lb *VarLeakyBucket[TKey]) Level(tnow time.Time) TLevel {
	diff := tnow.Sub(lb.lastAccessTime)
	var leaked = int64(lb.leakRate * float64(diff) / float64(lb.leakInterval))
	var currLevel = max(0, int64(lb.level)-leaked)
	return TLevel(currLevel)
}

func (lb *VarLeakyBucket[TKey]) Add(tnow time.Time, n TLevel) (TLevel, TLevel) {
	diff := tnow.Sub(lb.lastAccessTime)
	intervals := max(diff/lb.leakInterval, 0)
	var leaked = int64(lb.leakRate * float64(intervals))
	if diff > 0 {
		lb.lastAccessTime = tnow.Truncate(lb.leakInterval)
	}

	var currLevel = max(0, int64(lb.level)-leaked)
	var nextLevel = min(int64(lb.capacity), currLevel+int64(n))
	lb.level = TLevel(nextLevel)

	// this is the way of saying "is this the new {leakRate} interval at {tnow}"
	// NOTE: we are interested in sign of the {pendingCount} as diff can be negative
	if pendingCount := (diff / lb.leakInterval); pendingCount > 0 {
		lb.count += uint64(pendingCount)

		// unlikely uint64 "overflow" protection
		if lb.count == 0 {
			lb.count = 1
			lb.leakRate = 0.0
		}

		// we multiply mean by pendingCount accordingly to the formula of calculating mean if we skipped some elements
		// M[k] = (x[k] + x[k-1] + ... + x[1]) / k
		// M[k] = (x[k] + (k-1)*M[k-1]) / k     (k*M[k] just gives the sum of k elements)
		// or, if we will skip x[k-1] and x[k-2] elements (they are 0)
		// M[k] = (x[k] + x[k-1] + x[k-2] + (k-3)*M[k-3]) / k (and so on)
		// elements for us are zeroes for "missing" time windows without a value
		lb.leakRate = lb.leakRate + (float64(lb.pendingSum)-float64(pendingCount)*lb.leakRate)/(float64(lb.count))
		lb.pendingSum = 0
	}

	// {pendingSum} should be updated AFTER we update the {leakRate}. There are 2 cases:
	// 1. we add element to the "current/last" bucket, so we need to accumulate {pendingSum} and do nothing
	// 2. we add element to the _new_ bucket, so if we increment {pendingSum} before calculating {leakRate},
	//    we will will "split" the sum of the bucket into calculating now and in future, when we cross the boundary
	//    again, so this will make {pendingCount} incorrectly taken into account (like "twice")
	lb.pendingSum += int64(n)

	return TLevel(nextLevel), TLevel(nextLevel - currLevel)
}
