package leakybucket

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
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
	LeakRate   float64
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
		existing.Update(capacity, leakInterval, tnow)
		return existing, otter.WriteOp
	})
	if found {
		m.buckets.SetExpiresAfter(key, time.Duration(capacity)*leakInterval)
	}
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

	result.LeakRate = bucket.LeakRate()
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
	if !bu.result.Found {
		m.buckets.SetExpiresAfter(key, time.Duration(bu.capacity)*bu.leakInterval)
	}

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

func (m *Manager[TKey, T, TBucket]) SaveCache(ctx context.Context, dir, filename string, maxItems int, tnow time.Time) error {
	filter := func(b TBucket) bool {
		// we only save buckets that are not "full" (which means level > 0, so there is some usage)
		// NOTE: this is somewhat unfair for VarLeakyBucket beacause we discard learned leakRate/pendingSum/count combo
		return b.Level(tnow) > 0
	}
	return common.SaveCacheToFile(ctx, dir, filename, maxItems, m.buckets, filter)
}

func (m *Manager[TKey, T, TBucket]) LoadCache(ctx context.Context, dir, filename string) error {
	m.mu.RLock()
	cap64 := int64(m.capacity)
	interval := int64(m.leakInterval)
	ttl := time.Duration(math.MaxInt64)
	if cap64 != 0 && interval != 0 && cap64 <= math.MaxInt64/interval {
		ttl = time.Duration(cap64 * interval)
	}
	m.mu.RUnlock()

	return common.LoadCacheFromFile(ctx, dir, filename, ttl, m.buckets)
}
