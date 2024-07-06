package leakybucket

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestManagerCleanup(t *testing.T) {
	const maxBuckets = 8
	const cap = 5
	manager := NewManager[int32, ConstLeakyBucket[int32], *ConstLeakyBucket[int32]](maxBuckets, cap, 1*time.Second)
	tnow := time.Now().Truncate(1 * time.Second)
	// we add in reverse to check that we cleanup in correct order
	for i := maxBuckets - 1; i >= 0; i-- {
		key := int32(i)
		result := manager.Add(key, cap, tnow.Add(time.Duration(-i*10)*time.Millisecond))

		if result.Added != cap {
			t.Errorf("Unexpected added: %v (bucket %v)", result.Added, key)
		}

		if result.CurrLevel != cap {
			t.Errorf("Unexpected curr level: %v (bucket %v)", result.CurrLevel, key)
		}
	}

	deletedKeys := make([]int32, 0)
	callback := func(ctx context.Context, bucket LeakyBucket[int32]) {
		deletedKeys = append(deletedKeys, bucket.Key())
	}

	// lowerbound is 3/4 * maxbuckets == 6
	// we always compress to lowerbound first, so we should delete 2 at least
	// and then we add 2 more (to make 4) to be deleted from the "usual" ones
	manager.Cleanup(context.TODO(), tnow.Add(cap*time.Second), 4 /*maxToDelete*/, callback)

	// oldest items should be the last in the list based on BucketsHeap
	if !slices.Equal(deletedKeys, []int32{7, 6, 5, 4}) {
		t.Errorf("Unexpected keys were deleted: %v", deletedKeys)
	}
}
