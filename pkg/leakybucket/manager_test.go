package leakybucket

import (
	"net/netip"
	"sync"
	"testing"
	"time"
)

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
	const count = 500_000
	const key = 123

	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, count, 1*time.Second)
	tnow := time.Now().Truncate(1 * time.Second)

	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
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
	if result.CurrLevel != count {
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

func TestManagerConcurrentUpdate(t *testing.T) {
	const maxBuckets = 8
	const cap = 100
	const key = int32(42)

	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, cap, 1*time.Second)
	tnow := time.Now().Truncate(1 * time.Second)

	// Fill the bucket to level 10
	for i := 0; i < 10; i++ {
		manager.Add(key, 1, tnow)
	}

	level, ok := manager.Level(key, tnow)
	if !ok || level != 10 {
		t.Fatalf("Expected level 10, got %v (ok=%v)", level, ok)
	}

	// Run Update and Add concurrently to verify no data race
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			manager.Add(key, 1, tnow)
		}()
		go func() {
			defer wg.Done()
			manager.Update(key, cap, 1*time.Second, tnow)
		}()
	}
	wg.Wait()
}

func TestManagerUpdateSettlesBucketBeforeRateChange(t *testing.T) {
	const maxBuckets = 8
	const cap = 1000
	const key = int32(1)

	leakInterval := 10 * time.Second
	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, cap, leakInterval)

	t0 := time.Now().Truncate(leakInterval)

	// Fill to level 90
	for i := 0; i < 90; i++ {
		manager.Add(key, 1, t0)
	}

	level, _ := manager.Level(key, t0)
	if level != 90 {
		t.Fatalf("Expected level 90 at t0, got %v", level)
	}

	// Simulate 100 seconds passing, then change to a much faster leak rate (1 level/second)
	t1 := t0.Add(100 * time.Second)
	newLeakInterval := 1 * time.Second

	// Update must first settle the bucket (apply 10 leaked levels for 100s at old 10s rate),
	// then change the leak interval. So the level after settle should be 90 - 10 = 80.
	manager.Update(key, cap, newLeakInterval, t1)

	// At t1, after update, settled level should be max(0, 90-10) = 80
	level, _ = manager.Level(key, t1)
	if level != 80 {
		t.Errorf("Expected settled level 80 after Update at t1, got %v", level)
	}
}

func TestManagerConcurrentSetGlobalLimits(t *testing.T) {
	const maxBuckets = 8
	const key = int32(7)

	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, 100, 1*time.Second)
	tnow := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < 500; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			manager.Add(key, 1, tnow)
		}()
		go func() {
			defer wg.Done()
			manager.SetGlobalLimits(100, 1*time.Second)
		}()
	}
	wg.Wait()
}

func TestManagerIPAddrAddDefault(t *testing.T) {
	const maxBuckets = 8
	const cap = 5
	manager := NewManager[netip.Addr, ConstLeakyBucket[netip.Addr]](maxBuckets, cap, 1*time.Second)

	tnow := time.Now().Truncate(1 * time.Second)

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
