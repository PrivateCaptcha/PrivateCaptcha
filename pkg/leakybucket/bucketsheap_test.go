package leakybucket

import (
	"container/heap"
	"testing"
	"time"
)

func TestBucketsHeap(t *testing.T) {
	tnow := time.Now()
	var queue BucketsHeap[int32]

	heap.Init(&queue)

	b1 := &VarLeakyBucket[int32]{
		ConstLeakyBucket: ConstLeakyBucket[int32]{
			key:               2,
			lastAccessTime:    tnow.Add(-2 * time.Second),
			capacity:          10,
			leakRatePerSecond: 1,
			level:             3,
		},
	}

	heap.Push(&queue, b1)

	b2 := &VarLeakyBucket[int32]{
		ConstLeakyBucket: ConstLeakyBucket[int32]{
			key:               1,
			lastAccessTime:    tnow.Add(-1 * time.Second),
			capacity:          10,
			leakRatePerSecond: 1,
			level:             9,
		},
	}

	heap.Push(&queue, b2)

	if queue[0].Key() != 1 {
		t.Errorf("Unexpected first element key: %v", queue[0].Key())
	}

	queue[0].Add(tnow, 2)
	heap.Fix(&queue, 0)

	if queue[0].Key() != 1 {
		t.Errorf("Unexpected first element key after upadte: %v", queue[0].Key())
	}
}
