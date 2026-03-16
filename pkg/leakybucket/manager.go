package leakybucket

import (
	"context"
	"sync"
	"time"

	"github.com/maypok86/otter/v2"
)

type BucketConstraint[TKey comparable, T any] interface {
	LeakyBucket[TKey]
	*T
}

type BucketCallback[TKey comparable] func(context.Context, LeakyBucket[TKey])

type Manager[TKey comparable, T any, TBucket BucketConstraint[TKey, T]] struct {
	buckets      *otter.Cache[TKey, TBucket]
	mu           sync.RWMutex
	capacity     TLevel
	leakInterval time.Duration
}

type AddResult struct {
	CurrLevel  TLevel
	Added      TLevel
	Capacity   TLevel
	ResetAfter time.Duration
	RetryAfter time.Duration
	Found      bool
}

func (r *AddResult) Remaining() TLevel {
	return r.Capacity - r.CurrLevel
}

func NewManager[TKey comparable, T any, TBucket BucketConstraint[TKey, T]](maxBuckets int, capacity TLevel, leakInterval time.Duration) *Manager[TKey, T, TBucket] {
	return &Manager[TKey, T, TBucket]{
		buckets: otter.Must(&otter.Options[TKey, TBucket]{
			MaximumSize:      maxBuckets,
			InitialCapacity:  max(100, maxBuckets/1000),
			ExpiryCalculator: otter.ExpiryAccessing[TKey, TBucket](time.Duration(capacity) * leakInterval),
		}),
		capacity:     capacity,
		leakInterval: leakInterval,
	}
}

func (m *Manager[TKey, T, TBucket]) SetGlobalLimits(capacity TLevel, leakInterval time.Duration) {
	m.mu.Lock()
	m.capacity = capacity
	m.leakInterval = leakInterval
	m.mu.Unlock()
}

func (m *Manager[TKey, T, TBucket]) LeakInterval() time.Duration {
	m.mu.RLock()
	v := m.leakInterval
	m.mu.RUnlock()
	return v
}

func (m *Manager[TKey, T, TBucket]) Level(key TKey, tnow time.Time) (TLevel, bool) {
	bucket, ok := m.buckets.GetIfPresent(key)
	if !ok {
		return 0, false
	}

	return bucket.Level(tnow), true
}

func (m *Manager[TKey, T, TBucket]) Update(key TKey, capacity TLevel, leakInterval time.Duration, tnow time.Time) bool {
	found := false
	_, _ = m.buckets.Compute(key, func(existing TBucket, exists bool) (TBucket, otter.ComputeOp) {
		if !exists {
			return existing, otter.CancelOp
		}
		found = true
		// Settle the bucket to tnow using the old leak rate before changing the rate,
		// so the new rate is not applied retroactively to elapsed time.
		existing.Add(tnow, 0)
		existing.Update(capacity, leakInterval)
		return existing, otter.WriteOp
	})
	return found
}

type bucketUpdater[TKey comparable, T any, TBucket BucketConstraint[TKey, T]] struct {
	key          TKey
	capacity     TLevel
	leakInterval time.Duration
	tnow         time.Time
	n            TLevel
	result       AddResult
}

// NOTE: we use (abuse?) the fact that otter locks this cache bucket so we avoid having a mutex inside LeakyBucket(s) itself
func (bl *bucketUpdater[TKey, T, TBucket]) ComputeFunc(oldValue TBucket, found bool) (TBucket, otter.ComputeOp) {
	var bucket TBucket

	result := &bl.result

	result.Found = found

	if found {
		bucket = oldValue
	} else {
		bucket = new(T)
		bucket.Init(bl.key, bl.capacity, bl.leakInterval, bl.tnow)
	}

	result.CurrLevel, result.Added = bucket.Add(bl.tnow, bl.n)
	// 1 level each leakInterval
	leakInterval := bucket.LeakInterval()

	if result.Added > 0 {
		result.ResetAfter = time.Duration(result.CurrLevel) * leakInterval
	} else {
		result.RetryAfter = leakInterval
	}

	result.Capacity = bucket.Capacity()

	return bucket, otter.WriteOp
}

func (m *Manager[TKey, T, TBucket]) Add(key TKey, n TLevel, tnow time.Time) AddResult {
	if n == 0 {
		return AddResult{}
	}

	m.mu.RLock()
	capacity := m.capacity
	leakInterval := m.leakInterval
	m.mu.RUnlock()

	bu := &bucketUpdater[TKey, T, TBucket]{
		key:          key,
		capacity:     capacity,
		leakInterval: leakInterval,
		tnow:         tnow,
		n:            n,
	}

	_, _ = m.buckets.Compute(key, bu.ComputeFunc)

	return bu.result
}

func (m *Manager[TKey, T, TBucket]) AddEx(key TKey, n TLevel, tnow time.Time, initCapacity TLevel, initLeakInterval time.Duration) AddResult {
	if n == 0 {
		return AddResult{}
	}

	bu := &bucketUpdater[TKey, T, TBucket]{
		key:          key,
		capacity:     initCapacity,
		leakInterval: initLeakInterval,
		tnow:         tnow,
		n:            n,
	}

	_, _ = m.buckets.Compute(key, bu.ComputeFunc)

	if !bu.result.Found {
		m.buckets.SetExpiresAfter(key, time.Duration(bu.capacity)*bu.leakInterval)
	}

	return bu.result
}

func (m *Manager[TKey, T, TBucket]) Clear() {
	m.buckets.InvalidateAll()
}
