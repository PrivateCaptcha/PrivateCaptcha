package leakybucket

import (
	"container/heap"
	"sync"
	"time"

	"golang.org/x/exp/constraints"
)

type Manager[TKey constraints.Ordered] struct {
	buckets           map[TKey]*LeakyBucket[TKey]
	heap              BucketsHeap[TKey]
	lock              sync.Mutex
	capacity          int64
	leakRatePerSecond float64
	// if we overflow upperBound, we cleanup down to lowerBound
	upperBound int
	lowerBound int
}

func NewManager[TKey constraints.Ordered](bucketCapacity int64, maxBuckets int) *Manager[TKey] {
	m := &Manager[TKey]{
		buckets:    make(map[TKey]*LeakyBucket[TKey]),
		heap:       BucketsHeap[TKey]{},
		capacity:   bucketCapacity,
		upperBound: maxBuckets,
		lowerBound: maxBuckets/2 + maxBuckets/4,
	}

	heap.Init(&m.heap)

	return m
}

func (m *Manager[TKey]) Level(key TKey, tnow time.Time) (int64, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	bucket, ok := m.buckets[key]
	if !ok {
		return 0, false
	}

	return bucket.Level(tnow), true
}

func (m *Manager[TKey]) Backfill(bucket *LeakyBucket[TKey]) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if existing, ok := m.buckets[bucket.key]; ok {
		delete(m.buckets, existing.key)
		heap.Remove(&m.heap, existing.index)
	}

	m.buckets[bucket.key] = bucket
	heap.Push(&m.heap, bucket)
	m.ensureUpperBoundUnsafe()
}

func (m *Manager[TKey]) ensureUpperBoundUnsafe() {
	if (m.upperBound > 0) && (len(m.buckets) > m.upperBound) {
		last := m.heap.Last()
		if last != nil {
			// we delete just 1 item to stay within upperBound for performance reasons
			// elastic cleanup is done in Cleanup()
			delete(m.buckets, last.key)
			heap.Remove(&m.heap, last.index)
		}
	}
}

func (m *Manager[TKey]) Add(key TKey, n int64, tnow time.Time) (int64, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	var bucket *LeakyBucket[TKey]

	if existing, ok := m.buckets[key]; ok {
		bucket = existing
	} else {
		bucket = NewBucket[TKey](key, m.capacity, 0.0, tnow)
		m.buckets[key] = bucket
		heap.Push(&m.heap, bucket)
		m.ensureUpperBoundUnsafe()
	}

	added, err := bucket.Add(tnow, n)
	if added > 0 {
		heap.Fix(&m.heap, bucket.index)
	}

	return added, err
}

func (m *Manager[TKey]) compressUnsafe(cap int) int {
	if cap <= 0 {
		return 0
	}

	deleted := 0

	for len(m.buckets) > cap {
		last := m.heap.Last()
		if last != nil {
			// we delete just 1 item to stay within upperBound for performance reasons
			// elastic cleanup is done in Cleanup()
			delete(m.buckets, last.key)
			heap.Remove(&m.heap, last.index)
			deleted++
		} else {
			break
		}
	}

	return deleted
}

func (m *Manager[TKey]) Cleanup(tnow time.Time, maxToDelete int) int {
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

		delete(m.buckets, last.key)
		heap.Remove(&m.heap, last.index)
		maxToDelete++
	}

	return deleted
}

func (m *Manager[TKey]) Clear() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.buckets = make(map[TKey]*LeakyBucket[TKey])
	m.heap = BucketsHeap[TKey]{}
	heap.Init(&m.heap)
}
