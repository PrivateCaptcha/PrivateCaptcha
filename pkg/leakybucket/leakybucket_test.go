package leakybucket

import (
	"fmt"
	"math"
	randv2 "math/rand/v2"
	"testing"
	"time"
)

func TestVarLeakyBucketAdd(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewVarBucket[int32](0, 1234, 1*time.Second, tnow)

	for i := 0; i < 10; i++ {
		_, _ = bucket.Add(tnow.Add(time.Duration(i*100)*time.Millisecond), 1)
	}

	level := bucket.Level(tnow)
	if level != 10 {
		t.Errorf("Unexpected bucket level at time (t): %v", level)
	}

	// this should cause flush of the pendingSum and recalculation of the leak rate
	// also sets "last access time" to (t+1)
	_, added := bucket.Add(tnow.Add(1*time.Second), 1)
	if added != 1 {
		t.Errorf("Added unexpected amount: %v", added)
	}
	expectedLeakRate := 5.5
	if math.Abs(bucket.leakRate-expectedLeakRate) > 1e-3 {
		t.Errorf("Unexpected leak rate at time (t+1): %v, expected %v", bucket.leakRate, expectedLeakRate)
	}

	// for (t+1) leak rate is NOT recalculated again, but last access time is already (t+1) so leak not accounted
	level = bucket.Level(tnow.Add(1 * time.Second))
	if level != (10 + 1 - 1) {
		t.Errorf("Unexpected level at time (t+1): %v, (%+v)", level, bucket)
	}

	// now for (t+2) leak rate is finally taken into account
	level = bucket.Level(tnow.Add(2 * time.Second))
	if level != (10 + 1 - 1 - TLevel(expectedLeakRate)) {
		t.Errorf("Unexpected level at time (t+2): %v, (%+v)", level, bucket)
	}

	if math.Abs(bucket.leakRate-expectedLeakRate) > 1e-6 {
		t.Errorf("Unexpected leak rate of the bucket: %v", bucket.leakRate)
	}

	_, added = bucket.Add(tnow.Add(2*time.Second), 1)
	if added != 1 {
		t.Errorf("Added unexpected amount: %v", added)
	}
	// we added 10 at time t, 1 at t+1
	expectedLeakRate = (0 + 10 + 1 + 1) / 3.0
	if math.Abs(bucket.leakRate-expectedLeakRate) > 1e-3 {
		t.Errorf("Unexpected leak rate at time (t+3): %v, expected %v", bucket.leakRate, expectedLeakRate)
	}

	level = bucket.Level(tnow.Add(3 * time.Second))
	if expected := (10 + 1 - 1 - 5.0 + 1 - TLevel(expectedLeakRate)); level != expected {
		t.Errorf("Unexpected level at time (t+2): %v (expected %v)", level, expected)
	}
}

func TestLeakyBucketAddWithGap(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewVarBucket[int32](0, 1234, 1*time.Second, tnow)

	for i := 0; i < 10; i++ {
		_, _ = bucket.Add(tnow.Add(time.Duration(i*100)*time.Millisecond), 1)
	}

	level := bucket.Level(tnow)
	if level != 10 {
		t.Errorf("Unexpected bucket level at time (t): %v", level)
	}

	_, added := bucket.Add(tnow.Add(3*time.Second), 2)
	if added != 2 {
		t.Errorf("Added unexpected amount: %v", added)
	}

	// now we're added an item at time (t+3), it means that items (t+1) and (t+2) were 0 (missing)
	// and new item (2) is not yet taken into account
	expectedLeakRate := (1 + 10 + 0 + 0) / 4.0
	if math.Abs(bucket.leakRate-expectedLeakRate) > 1e-6 {
		t.Errorf("Unexpected leak rate of the bucket: %v (expected %v)", bucket.leakRate, expectedLeakRate)
	}

	level = bucket.Level(tnow.Add(3 * time.Second))
	if level != (10 - 1 - 1 - 1 + 2) {
		t.Errorf("Unexpected level at time (t+3): %v", level)
	}

	level = bucket.Level(tnow.Add(4 * time.Second))
	if level != (10 - 1 - 1 - 1 + 2 - TLevel(expectedLeakRate)) {
		t.Errorf("Unexpected level at time (t+4): %v", level)
	}
}

func TestLeakyBucketAddBulkAndSeparately(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucketBulk := NewVarBucket[int32](0, 1234, 1*time.Second, tnow)
	bucketSeparately := NewVarBucket[int32](0, 1234, 1*time.Second, tnow)

	count := 10

	for i := 0; i < count; i++ {
		_, _ = bucketSeparately.Add(tnow.Add(time.Duration(i*100)*time.Millisecond), 1)
	}

	_, _ = bucketBulk.Add(tnow.Add(500*time.Millisecond), uint32(count))

	ttime := tnow.Add(1 * time.Second)
	bulkLevel := bucketBulk.Level(ttime)
	separateLevel := bucketSeparately.Level(ttime)
	if bulkLevel != separateLevel {
		t.Errorf("Bucket levels are different. Bulk: %v Separate: %v", bulkLevel, separateLevel)
	}
}

func TestConstLeakyBucketAddOverflow(t *testing.T) {
	testCases := []struct {
		seconds  int
		value    uint32
		expected uint32
	}{
		{0, 0, 0},
		{1, 0, 0},
		{2, 0, 0},
		{4, 1, 1},
		{5, 0, 0},
		{7, math.MaxUint32 - 1, math.MaxUint32 - 1},
		{7, 1, 1},
		{7, 1, 0},
		{8, 1, 1},
	}

	tnow := time.Now()
	bucket := NewConstBucket[int32](0, math.MaxUint32, 1, tnow)

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("add_const_leaky_bucket_%v", i), func(t *testing.T) {
			_, actual := bucket.Add(tnow.Add(time.Duration(tc.seconds)*time.Second), tc.value)
			if actual != tc.expected {
				t.Errorf("Actual number added (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

func TestVarLeakyBucketResetTime(t *testing.T) {
	bucket := NewVarBucket[int32](0, 1234, 500*time.Millisecond, time.Now())
	bucket.leakRate = 2.0
	if bucket.LeakInterval() != 250*time.Millisecond {
		t.Errorf("Unexpected leak interval: %v", bucket.LeakInterval())
	}
}

func TestConstLeakyBucketAddWithinInterval(t *testing.T) {
	testCases := []struct {
		offset        time.Duration
		added         uint32
		expectedLevel uint32
	}{
		{0, 2, 2},
		{0, 3, 5},
		{10 * time.Millisecond, 1, 6},
		{20 * time.Millisecond, 1, 7},
		{101 * time.Millisecond, 1, 7 - 1 + 1},
	}

	tnow := time.Now().Truncate(100 * time.Millisecond)
	bucket := NewConstBucket[int32](0, 100 /*cap*/, 100*time.Millisecond, tnow)

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("add_within_interval_%v", i), func(t *testing.T) {
			_, _ = bucket.Add(tnow.Add(tc.offset), tc.added)
			level := bucket.Level(tnow.Add(tc.offset))
			if level != tc.expectedLevel {
				t.Errorf("Actual final level (%v) is different from expected (%v)", level, tc.expectedLevel)
			}
		})
	}
}

func TestLeakyBucketPastEvents(t *testing.T) {
	testCases := []struct {
		bucket LeakyBucket[int32]
	}{
		{&ConstLeakyBucket[int32]{}},
		{&VarLeakyBucket[int32]{}},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("leaky_bucket_past_%v", i), func(t *testing.T) {
			tnow := time.Now()
			tc.bucket.Init(123 /*id*/, math.MaxUint32, 1*time.Second, tnow)

			var prevLevel TLevel = 0

			for i := 1; i < 1000; i++ {
				currLevel, added := tc.bucket.Add(tnow.Add(-time.Duration(i*100)*time.Millisecond), 1)
				if added != 1 {
					t.Fatalf("Did not add 1 to the bucket. i=%v", i)
				}
				if currLevel <= prevLevel {
					t.Fatalf("Bucket level did not grow. curr=%v prev=%v i=%v", currLevel, prevLevel, i)
				}
				prevLevel = currLevel
			}
		})
	}
}

func TestVarLeakyBucketAverage(t *testing.T) {
	testCases := []struct {
		leakInterval      time.Duration
		iterationInterval time.Duration
		maxAddValue       int
		iterations        int
	}{
		{time.Second, time.Second, 100000, 1000000},
		{time.Second, 400 * time.Millisecond, 100000, 1000000},
		{time.Second, 350 * time.Millisecond, 100000, 1000000},
		{time.Second, 150 * time.Millisecond, 100000, 1000000},
		{400 * time.Millisecond, time.Second, 10000, 1000000},
		{350 * time.Millisecond, time.Second, 10000, 1000000},
		{150 * time.Millisecond, time.Second, 10000, 1000000},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("var_leaky_bucket_average_%v", i), func(t *testing.T) {
			tnow := time.Now().Truncate(min(tc.leakInterval, tc.iterationInterval))
			bucket := NewVarBucket[int32](123 /*id*/, math.MaxUint32, tc.leakInterval, tnow)
			var sum float64 = 0.0
			for i := 0; i < tc.iterations; i++ {
				addValue := randv2.IntN(tc.maxAddValue)
				sum += float64(addValue)
				_, _ = bucket.Add(tnow.Add(time.Duration(i)*tc.iterationInterval), uint32(addValue))
			}

			actual := bucket.leakRate

			iterationRate := float64(tc.iterationInterval) / float64(tc.leakInterval)
			expected := sum / (float64(tc.iterations) * iterationRate)

			if math.Abs(actual-expected) > max(1.0/iterationRate, iterationRate) {
				t.Errorf("Actual average (%v) is different from expected (%v)", actual, expected)
			}
		})
	}
}

func TestConstLeakyBucketGob(t *testing.T) {
	tnow := time.Now()
	b1 := NewConstBucket("test-key", 100, 10*time.Millisecond, tnow)
	// Apply some usage to alter state
	b1.Add(tnow, 50)

	enc, err := b1.GobEncode()
	if err != nil {
		t.Fatalf("GobEncode failed: %v", err)
	}

	b2 := &ConstLeakyBucket[string]{}
	err = b2.GobDecode(enc)
	if err != nil {
		t.Fatalf("GobDecode failed: %v", err)
	}

	if b1.key != b2.key {
		t.Errorf("key mismatch: %v != %v", b1.key, b2.key)
	}
	if b1.capacity != b2.capacity {
		t.Errorf("capacity mismatch: %v != %v", b1.capacity, b2.capacity)
	}
	if b1.leakInterval != b2.leakInterval {
		t.Errorf("leakInterval mismatch: %v != %v", b1.leakInterval, b2.leakInterval)
	}
	if b1.level != b2.level {
		t.Errorf("level mismatch: %v != %v", b1.level, b2.level)
	}
	if !b1.lastAccessTime.Equal(b2.lastAccessTime) {
		t.Errorf("lastAccessTime mismatch: %v != %v", b1.lastAccessTime, b2.lastAccessTime)
	}
}

func TestVarLeakyBucketGob(t *testing.T) {
	tnow := time.Now()
	b1 := NewVarBucket("test-key", 100, 10*time.Millisecond, tnow)
	// Apply some usage to alter state
	b1.Add(tnow, 50)

	// Alter internal VarLeakyBucket specific fields to test encoding
	b1.leakRate = 1.5
	b1.pendingSum = 10
	b1.count = 5

	enc, err := b1.GobEncode()
	if err != nil {
		t.Fatalf("GobEncode failed: %v", err)
	}

	b2 := &VarLeakyBucket[string]{}
	err = b2.GobDecode(enc)
	if err != nil {
		t.Fatalf("GobDecode failed: %v", err)
	}

	// Base fields
	if b1.key != b2.key || b1.capacity != b2.capacity || b1.leakInterval != b2.leakInterval || b1.level != b2.level || !b1.lastAccessTime.Equal(b2.lastAccessTime) {
		t.Errorf("Decoded base fields mismatch: got %+v, want %+v", b2, b1)
	}

	// Var specific fields
	if b1.leakRate != b2.leakRate {
		t.Errorf("leakRate mismatch: %v != %v", b1.leakRate, b2.leakRate)
	}
	if b1.pendingSum != b2.pendingSum {
		t.Errorf("pendingSum mismatch: %v != %v", b1.pendingSum, b2.pendingSum)
	}
	if b1.count != b2.count {
		t.Errorf("count mismatch: %v != %v", b1.count, b2.count)
	}
}

func TestOffGridUpdateSameIntervalAddLeaksPhantomInterval(t *testing.T) {
	const cap = 3
	key := int32(1)
	interval := 1 * time.Second
	manager := NewManager[int32, ConstLeakyBucket[int32]](8, cap, interval)

	t0 := time.Now().Truncate(interval)
	manager.Add(key, 1, t0)
	manager.Add(key, 1, t0) // level 2

	// Allowed request: middleware Add, then handler Update at an off-grid instant.
	manager.Add(key, 1, t0.Add(100*time.Millisecond))
	manager.Update(key, cap, interval, t0.Add(300*time.Millisecond)) // off-grid Update

	// Rejected request: Add in same interval, no Update follows.
	manager.Add(key, 1, t0.Add(500*time.Millisecond))

	// Only 0.9s since Update (< 1s interval), so bucket should still be full and reject.
	r3 := manager.Add(key, 1, t0.Add(1200*time.Millisecond))
	if r3.Added > 0 {
		t.Fatalf("BUG: allowed (Added=%v) but should be rejected (only 0.9s since Update)", r3.Added)
	}
}

func TestConstLeakyBucketUpdateClampsLevelOnCapacityDecrease(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewConstBucket[int32](0, 10, 1*time.Second, tnow)

	for i := 0; i < 10; i++ {
		bucket.Add(tnow, 1)
	}
	if bucket.level != 10 {
		t.Fatalf("expected level 10 after fill, got %v", bucket.level)
	}

	bucket.Update(5, 1*time.Second, tnow)

	if bucket.capacity != 5 {
		t.Errorf("expected capacity 5, got %v", bucket.capacity)
	}
	if bucket.level > bucket.capacity {
		t.Errorf("level %v exceeds capacity %v after Update", bucket.level, bucket.capacity)
	}
	if bucket.level != 5 {
		t.Errorf("expected level clamped to 5, got %v", bucket.level)
	}

	_, added := bucket.Add(tnow, 1)
	if added != 0 {
		t.Errorf("expected Added=0 after capacity lowered below level, got %v (uint32 underflow)", added)
	}
}

func TestConstLeakyBucketAddNoUnderflowWhenLevelExceedsCapacity(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewConstBucket[int32](0, 5, 1*time.Second, tnow)
	bucket.level = 10
	bucket.lastAccessTime = tnow

	_, added := bucket.Add(tnow, 1)
	if added != 0 {
		t.Errorf("Expected Added=0 when level(10) > capacity(5), got %v (uint32 underflow)", added)
	}
	if bucket.level != 5 {
		t.Errorf("Expected level clamped to capacity(5), got %v", bucket.level)
	}
}

func TestVarLeakyBucketAddNoUnderflowWhenLevelExceedsCapacity(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewVarBucket[int32](0, 5, 1*time.Second, tnow)
	bucket.level = 10
	bucket.lastAccessTime = tnow

	_, added := bucket.Add(tnow, 1)
	if added != 0 {
		t.Errorf("Expected Added=0 when level(10) > capacity(5), got %v (uint32 underflow)", added)
	}
	if bucket.level != 5 {
		t.Errorf("Expected level clamped to capacity(5), got %v", bucket.level)
	}
}

func TestVarLeakyBucketSeedNoUnderflowWhenLevelExceedsCapacity(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewVarBucket[int32](0, 5, 1*time.Second, tnow)
	bucket.level = 10
	bucket.lastAccessTime = tnow

	_, added := bucket.seed(tnow, 1, 1.0, 1)
	if added != 0 {
		t.Errorf("Expected Added=0 when level(10) > capacity(5), got %v (uint32 underflow)", added)
	}
}

// TestVarLeakyBucketFractionalLeakDrains reproduces the production bug for
// low-traffic properties post-backfill: a bucket seeded with leakRate = total/12 < 1
// (the case for any property with fewer than 12 requests in the last hour, the
// common case for staging/internal properties) would never drain because
// int64(leakRate * intervals) truncates to 0 on every Add while lastAccessTime
// advanced, discarding the fractional leak permanently and growing the level
// without bound. With leakResidual carrying the fractional credit, the bucket
// drains at approximately the running-mean rate.
func TestVarLeakyBucketFractionalLeakDrains(t *testing.T) {
	leakInterval := 5 * time.Minute
	t0 := time.Now().Truncate(leakInterval)
	bucket := NewVarBucket[int32](0, math.MaxUint32, leakInterval, t0)
	bucket.seed(t0, 12 /*level*/, 1.0/12.0 /*rate*/, 12 /*count*/)

	for i := 1; i <= 100; i++ {
		_, _ = bucket.Add(t0.Add(time.Duration(i)*leakInterval), 1)
	}

	// Without the fix: level == 12 + 100 == 112 (zero leak across all 100 Adds).
	// With the fix: level is well below 100 because fractional leak accumulates
	// and the bucket drains approximately at the running-mean rate r_k = k/(k+12).
	if bucket.level >= 100 {
		t.Errorf("Fractional leak not applied: level=%d after 100 Adds at leakRate<1 (want < 100, residual=%v)", bucket.level, bucket.leakResidual)
	}
	if bucket.level == 0 {
		t.Errorf("Bucket overdrained: level=0 after 100 Adds with leakRate<1 (residual=%v)", bucket.leakResidual)
	}
	// The carried residual must always stay in [0, 1) so each Add credits at most
	// one additional unit of leak. A residual >= 1 would mean we are double-counting.
	if bucket.leakResidual < 0 || bucket.leakResidual >= 1.0 {
		t.Errorf("leakResidual out of range [0, 1): %v", bucket.leakResidual)
	}
}

// TestVarLeakyBucketResidualCarriesFractionalLeak isolates the residual behavior.
// With leakRate = 0.5 an Add over exactly one interval produces leakFloat = 0.5
// and stores the remainder in leakResidual instead of discarding it. A second
// Add accumulates the carried 0.5 onto the new 0.5 fractional, reaching 1.0 and
// draining exactly one unit. A count >>= 1 keeps the running-mean update from
// meaningfully perturbing leakRate across the two-step test.
func TestVarLeakyBucketResidualCarriesFractionalLeak(t *testing.T) {
	interval := 1 * time.Second
	t0 := time.Now().Truncate(interval)
	bucket := NewVarBucket[int32](0, math.MaxUint32, interval, t0)
	bucket.leakRate = 0.5
	bucket.count = 1 << 60
	bucket.level = 10
	bucket.leakResidual = 0

	curr, added := bucket.Add(t0.Add(interval), 0)
	if added != 0 {
		t.Errorf("Add(n=0) returned added=%v, want 0", added)
	}
	if curr != 10 {
		t.Errorf("First Add should drain 0 (leakFloat=0.5 truncates), got level=%v (want 10)", curr)
	}
	if math.Abs(bucket.leakResidual-0.5) > 1e-9 {
		t.Errorf("Residual after first Add should be ~0.5, got %v", bucket.leakResidual)
	}

	curr, _ = bucket.Add(t0.Add(2*interval), 0)
	if curr != 9 {
		t.Errorf("Second Add should drain 1 (0.5 carried + 0.5 new = 1.0), got level=%v (want 9)", curr)
	}
	if math.Abs(bucket.leakResidual) > 1e-9 {
		t.Errorf("Residual after draining Add should be ~0, got %v", bucket.leakResidual)
	}
}

// TestVarLeakyBucketLevelMatchesAddAfterPartialLeak verifies the Level/Add
// divergence fix (the report's Evidence B). Previously Level() used the full
// fractional diff while Add() floored to whole intervals before computing the
// leak, so they reported different levels for the same wall-clock instant.
// After the fix both use the floored-intervals-plus-residual formula, so
// Level(t) returns the same value Add(t, 0) will persist into lb.level.
func TestVarLeakyBucketLevelMatchesAddAfterPartialLeak(t *testing.T) {
	interval := 1 * time.Second
	t0 := time.Now().Truncate(interval)
	bucket := NewVarBucket[int32](0, math.MaxUint32, interval, t0)
	bucket.leakRate = 1.5
	bucket.count = 1 << 60
	bucket.level = 50
	bucket.leakResidual = 0

	// Off-grid query time: 1.5 intervals after t0. The original Level() computed
	// leaked = int64(1.5 * 1.5) = 2 (full diff), while Add() computed
	// leaked = int64(1.5 * 1) = 1 (floored intervals); they disagreed.
	query := t0.Add(3 * interval / 2)

	reportedLevel := bucket.Level(query)
	addedLevel, _ := bucket.Add(query, 0)
	persistedLevel := bucket.level

	if reportedLevel != addedLevel {
		t.Errorf("Level(t)=%v != Add(t,0).CurrLevel=%v at the same wall-clock instant", reportedLevel, addedLevel)
	}
	if reportedLevel != persistedLevel {
		t.Errorf("Level(t)=%v != persisted lb.level=%v after Add(t, 0)", reportedLevel, persistedLevel)
	}
	if reportedLevel != 49 {
		t.Errorf("Expected drained level 49 (50 - 1), got %v", reportedLevel)
	}
}

// TestVarLeakyBucketGobDecodeLeavesResidualZero verifies that buckets encoded
// before the leakResidual field was introduced still decode cleanly and that
// leakResidual defaults to its zero value. The gob wire format is intentionally
// unchanged so existing on-disk caches keep loading; the residual rebuilds from
// the next Add onward, losing at most one unit of fractional leak credit.
func TestVarLeakyBucketGobDecodeLeavesResidualZero(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	b1 := NewVarBucket("test-key", 100, 10*time.Millisecond, tnow)
	b1.Add(tnow, 50)
	b1.leakRate = 1.5
	b1.pendingSum = 10
	b1.count = 5
	// Charge the residual to a non-zero value before encoding; the encoder does
	// not serialize it, so the decoder must report zero.
	b1.leakResidual = 0.75

	enc, err := b1.GobEncode()
	if err != nil {
		t.Fatalf("GobEncode failed: %v", err)
	}

	b2 := &VarLeakyBucket[string]{}
	if err := b2.GobDecode(enc); err != nil {
		t.Fatalf("GobDecode failed: %v", err)
	}

	if b2.leakResidual != 0 {
		t.Errorf("Decoded leakResidual = %v, want 0 (residual is not part of the gob wire format)", b2.leakResidual)
	}
	// Existing fields must round-trip intact so old caches keep loading.
	if b1.leakRate != b2.leakRate || b1.pendingSum != b2.pendingSum || b1.count != b2.count {
		t.Errorf("Decoded var fields mismatch: leakRate %v/%v, pendingSum %v/%v, count %v/%v",
			b2.leakRate, b1.leakRate, b2.pendingSum, b1.pendingSum, b2.count, b1.count)
	}
	if b1.level != b2.level || !b1.lastAccessTime.Equal(b2.lastAccessTime) {
		t.Errorf("Decoded base fields mismatch: level %v/%v, lastAccessTime %v/%v",
			b2.level, b1.level, b2.lastAccessTime, b1.lastAccessTime)
	}
}
