package leakybucket

import (
	"container/heap"
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
	// if we overflow upperBound, we cleanup down to lowerBound
	upperBound int
	lowerBound int
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

func (m *Manager[TKey, T, TBucket]) Add(key TKey, n TLevel, tnow time.Time) (TLevel, TLevel, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	var bucket TBucket
	found := false

	if existing, ok := m.buckets[key]; ok {
		bucket = existing
		found = true
	} else {
		bucket = new(T)
		bucket.Init(key, m.capacity, m.leakRatePerSecond, tnow)
		m.buckets[key] = bucket
		heap.Push(&m.heap, bucket)
		m.ensureUpperBoundUnsafe()
	}

	level, added := bucket.Add(tnow, n)
	if added > 0 {
		heap.Fix(&m.heap, bucket.Index())
	}

	return level, added, found
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
