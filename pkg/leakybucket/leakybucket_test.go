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
