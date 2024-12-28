package leakybucket

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

type BucketConstraint[TKey comparable, T any] interface {
	LeakyBucket[TKey]
	*T
}

type BucketCallback[TKey comparable] func(context.Context, LeakyBucket[TKey])

type Manager[TKey comparable, T any, TBucket BucketConstraint[TKey, T]] struct {
	buckets      map[TKey]TBucket
	heap         BucketsHeap[TKey]
	lock         sync.Mutex
	capacity     TLevel
	leakInterval time.Duration
	// fallback rate limiting bucket for "default" key (usually, "empty" key). Unused if nil.
	// For example, it's utilized for http rate limiter when we don't have a reliable IP
	defaultBucket TBucket
	// if we overflow upperBound, we cleanup down to lowerBound
	upperBound int
	lowerBound int
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

	m := &Manager[TKey, T, TBucket]{
		buckets:      make(map[TKey]TBucket),
		heap:         BucketsHeap[TKey]{},
		capacity:     capacity,
		leakInterval: leakInterval,
		upperBound:   maxBuckets,
		lowerBound:   maxBuckets/2 + maxBuckets/4,
	}

	heap.Init(&m.heap)

	return m
}

func (m *Manager[TKey, T, TBucket]) SetDefaultBucket(bucket TBucket) {
	m.lock.Lock()
	m.defaultBucket = bucket
	m.lock.Unlock()
}

func (m *Manager[TKey, T, TBucket]) LeakInterval() time.Duration {
	return m.leakInterval
}

func (m *Manager[TKey, T, TBucket]) Level(key TKey, tnow time.Time) (TLevel, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	bucket, ok := m.buckets[key]
	if !ok {
		return 0, false
	}

	return bucket.Level(tnow), true
}

func (m *Manager[TKey, T, TBucket]) ensureUpperBoundUnsafe() {
	if (m.upperBound > 0) && (len(m.buckets) > m.upperBound) {
		last := m.heap.Peek()
		if last != nil {
			// we delete just 1 item to stay within upperBound for performance reasons
			// elastic cleanup is done in Cleanup()
			delete(m.buckets, last.Key())
			heap.Pop(&m.heap)
		}
	}
}

func (m *Manager[TKey, T, TBucket]) update(key TKey, capacity TLevel, leakInterval time.Duration) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.defaultBucket != nil && (m.defaultBucket.Key() == key) {
		m.defaultBucket.Update(capacity, leakInterval)
		return true
	}

	existing, ok := m.buckets[key]
	if ok {
		existing.Update(capacity, leakInterval)
	}

	return ok
}

func (m *Manager[TKey, T, TBucket]) UpdaterFunc(key TKey) LimitUpdaterFunc {
	return func(capacity TLevel, leakInterval time.Duration) {
		m.update(key, capacity, leakInterval)
	}
}

func (m *Manager[TKey, T, TBucket]) Add(key TKey, n TLevel, tnow time.Time) AddResult {
	result := AddResult{}

	if n == 0 {
		return result
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	var bucket TBucket

	if m.defaultBucket != nil && (m.defaultBucket.Key() == key) {
		bucket = m.defaultBucket
	} else {
		if existing, ok := m.buckets[key]; ok {
			bucket = existing
			result.Found = true
		} else {
			bucket = new(T)
			bucket.Init(key, m.capacity, m.leakInterval, tnow)
			m.buckets[key] = bucket
			heap.Push(&m.heap, bucket)
			m.ensureUpperBoundUnsafe()
		}
	}

	result.CurrLevel, result.Added = bucket.Add(tnow, n)
	// 1 level each leakInterval
	leakInterval := bucket.LeakInterval()

	if result.Added > 0 {
		heap.Fix(&m.heap, bucket.Index())
		result.ResetAfter = time.Duration(result.CurrLevel) * leakInterval
	} else {
		result.RetryAfter = leakInterval
	}

	result.Capacity = bucket.Capacity()

	return result
}

func (m *Manager[TKey, T, TBucket]) compressUnsafe(cap int, collect bool) ([]LeakyBucket[TKey], int) {
	if cap <= 0 {
		return []LeakyBucket[TKey]{}, 0
	}

	deleted := make([]LeakyBucket[TKey], 0)
	deletedCount := 0

	for len(m.buckets) > cap {
		last := m.heap.Peek()
		if last != nil {
			if collect {
				deleted = append(deleted, last)
			}

			delete(m.buckets, last.Key())
			heap.Pop(&m.heap)
			deletedCount++
		} else {
			break
		}
	}

	return deleted, deletedCount
}

func (m *Manager[TKey, T, TBucket]) cleanupImpl(tnow time.Time, maxToDelete int, collect bool) ([]LeakyBucket[TKey], int) {
	m.lock.Lock()
	defer m.lock.Unlock()

	compressCap := len(m.buckets) - maxToDelete
	if compressCap < m.lowerBound {
		compressCap = m.lowerBound
	}

	deleted, deletedCount := m.compressUnsafe(compressCap, collect)
	if deletedCount >= maxToDelete {
		return deleted, deletedCount
	}

	for (deletedCount < maxToDelete) && (len(m.heap) > 0) {
		last := m.heap.Peek()
		level := last.Level(tnow)
		if level > 0 {
			break
		}

		if collect {
			deleted = append(deleted, last)
		}

		delete(m.buckets, last.Key())
		heap.Pop(&m.heap)
		deletedCount++
	}

	return deleted, deletedCount
}

// Removes up to maxToDelete obsolete or expired records. Returns number of records actually deleted.
func (m *Manager[TKey, T, TBucket]) Cleanup(ctx context.Context, tnow time.Time, maxToDelete int, callback BucketCallback[TKey]) int {
	deleted, deletedCount := m.cleanupImpl(tnow, maxToDelete, callback != nil /*collect*/)

	if callback != nil {
		for _, bucket := range deleted {
			callback(ctx, bucket)
		}
	}

	return deletedCount
}

func (m *Manager[TKey, T, TBucket]) Clear() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.buckets = make(map[TKey]TBucket)
	m.heap = BucketsHeap[TKey]{}
	heap.Init(&m.heap)
}
