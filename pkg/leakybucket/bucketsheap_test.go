package leakybucket

import (
	"container/heap"
	"testing"
	"time"
)

func TestBucketsHeap(t *testing.T) {
	tnow := time.Now()
	var queue BucketsHeap[int32]

	queue = append(queue, &LeakyBucket[int32]{
		key:               2,
		lastAccessTime:    tnow.Add(-2 * time.Second),
		capacity:          10,
		leakRatePerSecond: 1,
		level:             3,
	})
	queue = append(queue, &LeakyBucket[int32]{
		key:               1,
		lastAccessTime:    tnow.Add(-1 * time.Second),
		capacity:          10,
		leakRatePerSecond: 1,
		level:             9,
	})

	heap.Init(&queue)

	if queue[0].key != 1 {
		t.Errorf("Unexpected first element key: %v", queue[0].key)
	}

	queue[0].Add(tnow, 2)
	heap.Fix(&queue, 0)

	if queue[0].key != 1 {
		t.Errorf("Unexpected first element key after upadte: %v", queue[0].key)
	}
}
