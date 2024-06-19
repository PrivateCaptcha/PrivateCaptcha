package leakybucket

import (
	"math"
	"testing"
	"time"
)

func TestLeakyBucketAdd(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewBucket[int32](0, 1234, 0, tnow)

	for i := 0; i < 10; i++ {
		_, _ = bucket.Add(tnow.Add(time.Duration(i*100)*time.Millisecond), 1)
	}

	level := bucket.Level(tnow)
	if level != 10 {
		t.Errorf("Unexpected bucket level at time (t): %v", level)
	}

	// this should cause flush of the pendingSum and recalculation of the leak rate
	// also sets "last access time" to (t+1)
	added, err := bucket.Add(tnow.Add(1*time.Second), 1)
	if added != 1 || err != nil {
		t.Errorf("Added unexpected amount: %v (err: %v)", added, err)
	}

	// for (t+1) leak rate is recalculated, but last access time is already (t+1) so leak not accounted
	level = bucket.Level(tnow.Add(1 * time.Second))
	if level != (10 + 1) {
		t.Errorf("Unexpected level at time (t+1): %v", level)
	}

	// now for (t+2) leak rate is finally taken into account
	level = bucket.Level(tnow.Add(2 * time.Second))
	expectedLeakRate := 5.5
	if level != (10 + 1 - int64(expectedLeakRate+0.5)) {
		t.Errorf("Unexpected level at time (t+2): %v", level)
	}

	if math.Abs(bucket.leakRatePerSecond-expectedLeakRate) > 1e-6 {
		t.Errorf("Unexpected leak rate of the bucket: %v", bucket.leakRatePerSecond)
	}

	added, err = bucket.Add(tnow.Add(2*time.Second), 1)
	if added != 1 || err != nil {
		t.Errorf("Added unexpected amount: %v (err: %v)", added, err)
	}

	level = bucket.Level(tnow.Add(2 * time.Second))
	if level != (10 + 1 - int64(expectedLeakRate+0.5) + 1) {
		t.Errorf("Unexpected level at time (t+2): %v", level)
	}
}

func TestLeakyBucketAddWithGap(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewBucket[int32](0, 1234, 0, tnow)

	for i := 0; i < 10; i++ {
		_, _ = bucket.Add(tnow.Add(time.Duration(i*100)*time.Millisecond), 1)
	}

	level := bucket.Level(tnow)
	if level != 10 {
		t.Errorf("Unexpected bucket level at time (t): %v", level)
	}

	added, err := bucket.Add(tnow.Add(3*time.Second), 2)
	if added != 2 || err != nil {
		t.Errorf("Added unexpected amount: %v (err: %v)", added, err)
	}

	// now we're added an item at time (t+3), it means that items (t+1) and (t+2) were 0 (missing)
	expectedLeakRate := (10 + 0 + 0 + 2) / 4.0
	if math.Abs(bucket.leakRatePerSecond-expectedLeakRate) > 1e-6 {
		t.Errorf("Unexpected leak rate of the bucket: %v", bucket.leakRatePerSecond)
	}

	level = bucket.Level(tnow.Add(3 * time.Second))
	if level != (10 + 2) {
		t.Errorf("Unexpected level at time (t+3): %v", level)
	}

	level = bucket.Level(tnow.Add(4 * time.Second))
	if level != (10 + 2 - int64(expectedLeakRate)) {
		t.Errorf("Unexpected level at time (t+4): %v", level)
	}
}

func TestLeakyBucketAddRetroactively(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewBucket[int32](0, 1234, 0, tnow)

	for i := 0; i < 10; i++ {
		_, err := bucket.Add(tnow.Add(time.Duration(i*100)*time.Millisecond), 1)
		if err != nil {
			t.Errorf("Failed to add item to the bucket: %v", err)
		}
	}

	_, err := bucket.Add(tnow.Add(-1*time.Second), 1)
	if err == nil {
		t.Errorf("Managed to add retroactively")
	}
}

func TestLeakyBucketAddBulkAndSeparately(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucketBulk := NewBucket[int32](0, 1234, 0, tnow)
	bucketSeparately := NewBucket[int32](0, 1234, 0, tnow)

	count := 10

	for i := 0; i < count; i++ {
		_, _ = bucketSeparately.Add(tnow.Add(time.Duration(i*100)*time.Millisecond), 1)
	}

	_, _ = bucketBulk.Add(tnow.Add(500*time.Millisecond), int64(count))

	ttime := tnow.Add(1 * time.Second)
	bulkLevel := bucketBulk.Level(ttime)
	separateLevel := bucketSeparately.Level(ttime)
	if bulkLevel != separateLevel {
		t.Errorf("Bucket levels are different. Bulk: %v Separate: %v", bulkLevel, separateLevel)
	}
}

func TestLeakyBucketBackfill(t *testing.T) {
	tnow := time.Now().Truncate(1 * time.Second)
	bucket := NewBucket[int32](0, 1234, 0, tnow)

	count := 10
	const requestsPerInterval = 300
	const interval = 5 * time.Minute

	for i := 0; i < count; i++ {
		bucket.Add(tnow.Add(time.Duration(i)*interval), requestsPerInterval)
	}

	expectedMean := requestsPerInterval / (5.0 * 60.0)

	if math.Abs(expectedMean-bucket.mean) > 1e-6 {
		t.Errorf("Unexpected mean after backfill: %v (expected %v)", bucket.mean, expectedMean)
	}
}
