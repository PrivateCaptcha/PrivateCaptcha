package leakybucket

import (
	"bytes"
	"context"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
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

func TestManagerUpdateRebasesTimestampOffBoundary(t *testing.T) {
	const maxBuckets = 8
	const cap = 1000
	const key = int32(2)

	oldInterval := 10 * time.Second
	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, cap, oldInterval)

	// Start at a 10s boundary, fill to level 20
	t0 := time.Now().Truncate(oldInterval)
	for i := 0; i < 20; i++ {
		manager.Add(key, 1, t0)
	}

	// Simulate 5 seconds into the next interval (off-boundary for oldInterval=10s)
	tUpdate := t0.Add(5 * time.Second)
	newInterval := 1 * time.Second

	// At tUpdate: elapsed = 5s at old 10s/level => 0 levels leaked (not a full interval).
	// Switch to 1s/level. After update, level stays 20.
	manager.Update(key, cap, newInterval, tUpdate)

	level, _ := manager.Level(key, tUpdate)
	if level != 20 {
		t.Errorf("Expected level 20 right after Update at tUpdate, got %v", level)
	}

	// 1 second later: should leak exactly 1 level (not 6 = tUpdate - t0.Truncate(oldInterval))
	tNext := tUpdate.Add(1 * time.Second)
	level, _ = manager.Level(key, tNext)
	if level != 19 {
		t.Errorf("Expected level 19 one second after Update, got %v (timestamp not rebased)", level)
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

func TestManagerCacheConstLeakyBucket(t *testing.T) {
	ctx := context.TODO()

	manager := NewManager[string, ConstLeakyBucket[string], *ConstLeakyBucket[string]](100, 10, 10*time.Millisecond)

	tnow := time.Now()
	manager.Add("user1", 5, tnow)
	manager.Add("user3", 10, tnow)

	var buf bytes.Buffer

	filter := func(b *ConstLeakyBucket[string]) bool {
		return b.Level(tnow) > 0
	}

	_, err := common.SaveCacheToWriter(ctx, &buf, manager.buckets, 100, filter)
	if err != nil {
		t.Fatalf("SaveCacheToWriter failed: %v", err)
	}

	manager2 := NewManager[string, ConstLeakyBucket[string], *ConstLeakyBucket[string]](100, 10, 10*time.Millisecond)
	err = common.LoadCacheFromReader(ctx, &buf, manager2.buckets, 24*time.Hour)
	if err != nil {
		t.Fatalf("LoadCacheFromReader failed: %v", err)
	}

	lvl1, ok1 := manager2.Level("user1", tnow)
	if !ok1 || lvl1 != 5 {
		t.Errorf("user1 expected level 5, got %v (ok=%v)", lvl1, ok1)
	}

	lvl3, ok3 := manager2.Level("user3", tnow)
	if !ok3 || lvl3 != 10 {
		t.Errorf("user3 expected level 10, got %v (ok=%v)", lvl3, ok3)
	}

	lvl1b, _ := manager2.Level("user1", tnow.Add(100*time.Millisecond))
	if lvl1b != 0 {
		t.Errorf("expected user1 level to leak to 0, got %v", lvl1b)
	}
}

func TestManagerCacheVarLeakyBucket(t *testing.T) {
	ctx := context.TODO()

	manager := NewManager[int32, VarLeakyBucket[int32], *VarLeakyBucket[int32]](100, 10, 10*time.Millisecond)

	tnow := time.Now()
	manager.Add(1, 5, tnow)
	manager.Add(2, 10, tnow)

	var buf bytes.Buffer

	filter := func(b *VarLeakyBucket[int32]) bool {
		return b.Level(tnow) > 0
	}

	_, err := common.SaveCacheToWriter(ctx, &buf, manager.buckets, 100, filter)
	if err != nil {
		t.Fatalf("SaveCacheToWriter failed: %v", err)
	}

	manager2 := NewManager[int32, VarLeakyBucket[int32], *VarLeakyBucket[int32]](100, 10, 10*time.Millisecond)
	err = common.LoadCacheFromReader(ctx, &buf, manager2.buckets, 24*time.Hour)
	if err != nil {
		t.Fatalf("LoadCacheFromReader failed: %v", err)
	}

	lvl1, ok1 := manager2.Level(1, tnow)
	if !ok1 || lvl1 != 5 {
		t.Errorf("1 expected level 5, got %v (ok=%v)", lvl1, ok1)
	}

	lvl2, ok2 := manager2.Level(2, tnow)
	if !ok2 || lvl2 != 10 {
		t.Errorf("2 expected level 10, got %v (ok=%v)", lvl2, ok2)
	}

	// Verify the updated limits persisted
	// Waiting 100ms should leak levels (since it leaks based on running rate, approx 1 per 10ms initially)
	lvl2b, _ := manager2.Level(2, tnow.Add(200*time.Millisecond))
	if lvl2b != 0 {
		t.Errorf("expected 2 level to leak to 0, got %v", lvl2b)
	}
}
