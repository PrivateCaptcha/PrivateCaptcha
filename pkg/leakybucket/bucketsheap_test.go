package leakybucket

import (
	"container/heap"
	"slices"
	"testing"
	"time"
)

func TestBucketsHeap(t *testing.T) {
	tnow := time.Now()
	var queue BucketsHeap[int32]

	heap.Init(&queue)

	b1 := &VarLeakyBucket[int32]{
		ConstLeakyBucket: ConstLeakyBucket[int32]{
			key:            1,
			lastAccessTime: tnow.Add(-2 * time.Second),
			capacity:       10,
			leakInterval:   1 * time.Second,
			level:          3,
		},
	}

	heap.Push(&queue, b1)

	b2 := &VarLeakyBucket[int32]{
		ConstLeakyBucket: ConstLeakyBucket[int32]{
			key:            2,
			lastAccessTime: tnow.Add(-1 * time.Second),
			capacity:       10,
			leakInterval:   1 * time.Second,
			level:          9,
		},
	}

	// b2 should become first as it has "earliest" access time
	heap.Push(&queue, b2)

	if queue[0].Key() != 1 {
		t.Errorf("Unexpected first element key: %v", queue[0].Key())
	}

	queue[0].Add(tnow, 2)
	heap.Fix(&queue, 0)

	// now b2 should be the second as we just updated last access time for the second element
	if queue[0].Key() != 2 {
		t.Errorf("Unexpected first element key after upadte: %v", queue[0].Key())
	}
}

func TestHeapOrdering(t *testing.T) {
	tnow := time.Now()
	var queue BucketsHeap[int32]
	heap.Init(&queue)

	const maxBuckets = 8

	for i := maxBuckets - 1; i >= 0; i-- {
		// bucket with the largest key is also the furthest in time from tnow
		bucket := NewConstBucket[int32](int32(i), 5 /*capacity*/, 1.0 /*leakRate*/, tnow.Add(time.Duration(-i*10)*time.Millisecond))
		heap.Push(&queue, bucket)
	}

	unfoldedHeap := make([]int32, 0)
	for len(queue) > 0 {
		item := heap.Pop(&queue)
		unfoldedHeap = append(unfoldedHeap, item.(LeakyBucket[int32]).Key())
	}

	// oldest items should be the first in the list based on BucketsHeap Less()
	if !slices.Equal(unfoldedHeap, []int32{7, 6, 5, 4, 3, 2, 1, 0}) {
		t.Errorf("Unexpected heap: %v", unfoldedHeap)
	}
}
