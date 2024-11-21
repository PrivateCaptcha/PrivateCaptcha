package leakybucket

import (
	"context"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestManagerCleanup(t *testing.T) {
	const maxBuckets = 8
	const cap = 5
	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, cap, 1*time.Second)
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

func TestManagerAdd(t *testing.T) {
	const maxBuckets = 8
	const cap = 5
	const key = 123

	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, cap, 1*time.Second)
	tnow := time.Now().Truncate(1 * time.Second)

	for i := 0; i < cap; i++ {
		result := manager.Add(key, 1, tnow)
		if result.CurrLevel != uint32(i+1) {
			t.Errorf("Unexpected level: %v", result.CurrLevel)
		}
		if result.Added != 1 {
			t.Errorf("Failed to add to bucket")
		}
	}
}

func TestManagerAddParallel(t *testing.T) {
	const maxBuckets = 8
	const cap = 5
	const key = 123

	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, cap, 1*time.Second)
	tnow := time.Now().Truncate(1 * time.Second)

	var wg sync.WaitGroup

	for i := 0; i < cap; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			result := manager.Add(key, 1, tnow)
			if result.Added != 1 {
				t.Errorf("Failed to add to bucket")
			}
		}()
	}

	wg.Wait()

	result := manager.Add(key, 1, tnow)
	if result.CurrLevel != cap {
		t.Errorf("Unexpected level after full: %v", result.CurrLevel)
	}
	if result.Added != 0 {
		t.Errorf("Was able to add to the bucket after")
	}
}

func TestManagerAddDefault(t *testing.T) {
	const maxBuckets = 8
	const cap = 5
	const key = 123

	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, cap, 1*time.Second)
	tnow := time.Now().Truncate(1 * time.Second)

	manager.SetDefaultBucket(NewConstBucket[int32](key, cap, 1*time.Second, tnow.Add(-1*time.Minute)))

	for i := 0; i < cap; i++ {
		result := manager.Add(key, 1, tnow)
		if result.CurrLevel != uint32(i+1) {
			t.Errorf("Unexpected level: %v", result.CurrLevel)
		}
		if result.Added != 1 {
			t.Errorf("Failed to add to bucket")
		}
	}

	result := manager.Add(key, 1, tnow)
	if result.CurrLevel != cap {
		t.Errorf("Unexpected level after full: %v", result.CurrLevel)
	}
	if result.Added != 0 {
		t.Errorf("Managed to add to full bucket")
	}
}

func TestManagerIPAddrAddDefault(t *testing.T) {
	const maxBuckets = 8
	const cap = 5
	manager := NewManager[netip.Addr, ConstLeakyBucket[netip.Addr]](maxBuckets, cap, 1*time.Second)

	tnow := time.Now().Truncate(1 * time.Second)
	manager.SetDefaultBucket(NewConstBucket(netip.Addr{}, cap, 1.0 /*leakRatePerSecond*/, tnow))

	key := netip.Addr{}
	for i := 0; i < cap; i++ {
		result := manager.Add(key, 1, tnow)
		if result.CurrLevel != uint32(i+1) {
			t.Errorf("Unexpected level: %v", result.CurrLevel)
		}
		if result.Added != 1 {
			t.Errorf("Failed to add to bucket")
		}
	}

	result := manager.Add(key, 1, tnow)
	if result.CurrLevel != cap {
		t.Errorf("Unexpected level after full: %v", result.CurrLevel)
	}
	if result.Added != 0 {
		t.Errorf("Managed to add to full bucket")
	}
}
