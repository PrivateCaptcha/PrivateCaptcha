package leakybucket

import (
	"container/heap"
	"math"
	"sync"
	"time"
)

type BucketConstraint[TKey comparable, T any] interface {
	LeakyBucket[TKey]
	*T
}

type Manager[TKey comparable, T any, TBucket BucketConstraint[TKey, T]] struct {
	buckets           map[TKey]TBucket
	heap              BucketsHeap[TKey]
	lock              sync.Mutex
	capacity          TLevel
	leakRatePerSecond float64
	// fallback rate limiting bucket for "default" key (usually, "empty" key). Unused if nil.
	// For example, it's utilized for http rate limiter when we don't have a reliable IP
	defaultBucket TBucket
	// if we overflow upperBound, we cleanup down to lowerBound
	upperBound int
	lowerBound int
}

type AddResult struct {
	Level      TLevel
	Added      TLevel
	Capacity   TLevel
	ResetAfter time.Duration
	RetryAfter time.Duration
	Found      bool
}

func (r *AddResult) CurrentLevel() TLevel {
	return r.Level + r.Added
}

func (r *AddResult) Remaining() TLevel {
	return r.Capacity - r.Level - r.Added
}

func NewManager[TKey comparable, T any, TBucket BucketConstraint[TKey, T]](maxBuckets int, capacity TLevel, leakRatePerSecond float64) *Manager[TKey, T, TBucket] {
	m := &Manager[TKey, T, TBucket]{
		buckets:           make(map[TKey]TBucket),
		heap:              BucketsHeap[TKey]{},
		capacity:          capacity,
		leakRatePerSecond: leakRatePerSecond,
		upperBound:        maxBuckets,
		lowerBound:        maxBuckets/2 + maxBuckets/4,
	}

	heap.Init(&m.heap)

	return m
}

func (m *Manager[TKey, T, TBucket]) SetDefaultBucket(bucket TBucket) {
	m.lock.Lock()
	m.defaultBucket = bucket
	m.lock.Unlock()
}

func (m *Manager[TKey, T, TBucket]) Level(key TKey, tnow time.Time) (int64, bool) {
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
		last := m.heap.Last()
		if last != nil {
			// we delete just 1 item to stay within upperBound for performance reasons
			// elastic cleanup is done in Cleanup()
			delete(m.buckets, last.Key())
			heap.Remove(&m.heap, last.Index())
		}
	}
}

func resetTime(level TLevel, leakRatePerSecond float64) time.Duration {
	if math.Abs(leakRatePerSecond) < 1e-6 {
		if level == 0 {
			return 0
		}

		return 365 * 24 * time.Hour
	}

	seconds := float64(level) / leakRatePerSecond
	return time.Duration(seconds) * time.Second
}

func (m *Manager[TKey, T, TBucket]) Update(key TKey, capacity TLevel, leakRatePerSecond float64) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	existing, ok := m.buckets[key]
	if ok {
		existing.Update(capacity, leakRatePerSecond)
	}

	return ok
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
			bucket.Init(key, m.capacity, m.leakRatePerSecond, tnow)
			m.buckets[key] = bucket
			heap.Push(&m.heap, bucket)
			m.ensureUpperBoundUnsafe()
		}
	}

	result.Level, result.Added = bucket.Add(tnow, n)
	leakRate := bucket.LeakRatePerSecond()

	if result.Added > 0 {
		heap.Fix(&m.heap, bucket.Index())
		result.ResetAfter = resetTime(result.Level+result.Added, leakRate)
	} else {
		result.RetryAfter = resetTime(1 /*level*/, leakRate)
	}

	result.Capacity = bucket.Capacity()

	return result
}

func (m *Manager[TKey, T, TBucket]) compressUnsafe(cap int) int {
	if cap <= 0 {
		return 0
	}

	deleted := 0

	for len(m.buckets) > cap {
		last := m.heap.Last()
		if last != nil {
			// we delete just 1 item to stay within upperBound for performance reasons
			// elastic cleanup is done in Cleanup()
			delete(m.buckets, last.Key())
			heap.Remove(&m.heap, last.Index())
			deleted++
		} else {
			break
		}
	}

	return deleted
}

// Removes up to maxToDelete obsolete or expired records. Returns number of records actually deleted.
func (m *Manager[TKey, T, TBucket]) Cleanup(tnow time.Time, maxToDelete int) int {
	m.lock.Lock()
	defer m.lock.Unlock()

	compressCap := len(m.buckets) - maxToDelete
	if compressCap < m.lowerBound {
		compressCap = m.lowerBound
	}

	deleted := m.compressUnsafe(compressCap)
	if deleted >= maxToDelete {
		return deleted
	}

	for (deleted < maxToDelete) && (m.heap.Last() != nil) {
		last := m.heap.Last()
		level := last.Level(tnow)
		if level > 0 {
			break
		}

		delete(m.buckets, last.Key())
		heap.Remove(&m.heap, last.Index())
		maxToDelete++
	}

	return deleted
}

func (m *Manager[TKey, T, TBucket]) Clear() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.buckets = make(map[TKey]TBucket)
	m.heap = BucketsHeap[TKey]{}
	heap.Init(&m.heap)
}
