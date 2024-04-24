package difficulty

import (
	"slices"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	testBucketSize = 5 * time.Minute
)

func TestUserStatsInc(t *testing.T) {
	t.Parallel()

	stats := newUserStats()

	tnow := time.Now()
	key := common.RandomFingerprint()

	if st, ok := stats.Count(key, tnow, testBucketSize); ok || st != 0 {
		t.Errorf("Unexpected empty stats: %v", st)
	}

	for i := 0; i < 10; i++ {
		if st := stats.Inc(key, tnow, testBucketSize); int(st) != (i + 1) {
			t.Errorf("Unexpected stats: %v (iteration %v)", st, i)
		}
	}

	// increment "old" bucket should not have any effect
	if st := stats.Inc(key, tnow.Add(-testBucketSize).Add(-1*time.Second), testBucketSize); st != 10 {
		t.Errorf("Unexpected stats after old increment: %v", st)
	}

	if st, ok := stats.Count(key, tnow, testBucketSize); !ok || st != 10 {
		t.Errorf("Stats() result does not equal to Inc() result")
	}

	if st := stats.Inc(key, tnow.Add(testBucketSize).Add(1*time.Second), testBucketSize); st != 1 {
		t.Errorf("Unexpected stats for next time bucket: %v", st)
	}
}

func TestUserStatsBackfill(t *testing.T) {
	t.Parallel()

	stats := newUserStats()

	tnow := time.Now()
	key := common.RandomFingerprint()

	for i := 0; i < 10; i++ {
		_ = stats.Inc(key, tnow, testBucketSize)
	}

	if st, _ := stats.Count(key, tnow, testBucketSize); st != 10 {
		t.Errorf("Count() result is not 10")
	}

	stats.Backfill(key, []*common.TimeCount{{Timestamp: tnow, Count: 9}}, testBucketSize)

	if st, _ := stats.Count(key, tnow, testBucketSize); st != 10 {
		t.Errorf("Backfill overwrote with lower value")
	}

	stats.Backfill(key, []*common.TimeCount{{Timestamp: tnow, Count: 11}}, testBucketSize)

	if st, _ := stats.Count(key, tnow, testBucketSize); st != 11 {
		t.Errorf("Backfill did not overwrite with higher value")
	}
}

func TestUserStatsCleanup(t *testing.T) {
	t.Parallel()

	stats := newUserStats()

	tnow := time.Now()
	key := common.RandomFingerprint()

	for i := 0; i < 10; i++ {
		_ = stats.Inc(key, tnow, testBucketSize)
	}

	if st, _ := stats.Count(key, tnow, testBucketSize); st != 10 {
		t.Errorf("Stats() result is not 10")
	}

	if deleted := stats.Cleanup(tnow, testBucketSize, 10); deleted != 0 {
		t.Errorf("Unexpected deleted count: %v", deleted)
	}

	if deleted := stats.Cleanup(tnow.Add(testBucketSize).Add(1*time.Second), testBucketSize, 10); deleted != 1 {
		t.Errorf("Unexpected deleted count: %v", deleted)
	}

	if st, _ := stats.Count(key, tnow, testBucketSize); st != 0 {
		t.Errorf("Stats() result is not 0 after delete")
	}
}

func TestPropertyStatsInc(t *testing.T) {
	t.Parallel()

	tnow := time.Now()
	pid := int32(12345)
	fingerprint := common.RandomFingerprint()
	counts := newCounts(testBucketSize)

	for i := 0; i < 10; i++ {
		if st := counts.Inc(pid, fingerprint, tnow); int(st) != (i + 1) {
			t.Errorf("Unexpected stats: %v (iteration %v)", st, i)
		}
	}

	if stats := counts.FetchStats(pid, common.RandomFingerprint(), tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{0, 0, 0, 0, 10}) {
		t.Errorf("Unexpected counts after increment: %v", stats.Property)
	}

	// now we set 1 request per each previous bucket
	for i := 0; i < 5; i++ {
		if st := counts.Inc(pid, common.RandomFingerprint(), tnow.Add(-time.Duration(i+1)*testBucketSize).Add(-time.Second)); st != 1 {
			t.Errorf("Unexpected stats: %v (iteration %v)", st, i)
		}
	}

	stats := counts.FetchStats(pid, fingerprint, tnow)
	if !stats.HasProperty || !slices.Equal(stats.Property, []uint32{1, 1, 1, 1, 10}) {
		t.Errorf("Unexpected property counts after increment: %v", stats.Property)
	}

	if !stats.HasFingerprint || (stats.Fingerprint != 10) {
		t.Errorf("Unexpected user counts after increment: %v", stats.Fingerprint)
	}
}

func TestPropertyStatsCleanup(t *testing.T) {
	t.Parallel()

	tnow := time.Now()
	pid := int32(12345)
	fingerprint := common.RandomFingerprint()

	counts := newCounts(testBucketSize)

	// now we set 1 request per each bucket
	for i := 0; i < 5; i++ {
		if st := counts.Inc(pid, fingerprint, tnow.Add(-time.Duration(i)*testBucketSize).Add(-time.Second)); st != 1 {
			t.Errorf("Unexpected stats: %v (iteration %v)", st, i)
		}
	}

	if stats := counts.FetchStats(pid, fingerprint, tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{1, 1, 1, 1, 1}) {
		t.Errorf("Unexpected counts after increment: %v", counts)
	}

	// for 5 buckets, their intervals are like so:
	// ... | 4 (t-4) | 3 (t-3) | 2 (t-2) | 1 (t-1) | 0 (t)
	// so if we clean from (t-2), it means last 3 buckets will be 0
	if deleted := counts.Cleanup(tnow, 2, 10); deleted != 0 {
		t.Errorf("Unexpected amount of properties deleted: %v", deleted)
	}

	if stats := counts.FetchStats(pid, fingerprint, tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{0, 0, 0, 1, 1}) {
		t.Errorf("Unexpected counts after cleanup: %v", counts)
	}

	if deleted := counts.Cleanup(tnow, 0, 10); deleted != 1 {
		t.Errorf("Unexpected amount of properties deleted: %v", deleted)
	}
}

func TestPropertyStatsBackfill(t *testing.T) {
	t.Parallel()

	tnow := time.Now()
	pid := int32(12345)
	fingerprint := common.RandomFingerprint()

	counts := newCounts(testBucketSize)

	// now we set 10 request per each bucket
	for i := 0; i < 50; i++ {
		counts.Inc(pid, fingerprint, tnow.Add(-time.Duration(i%5)*testBucketSize).Add(-time.Second))
	}

	if stats := counts.FetchStats(pid, fingerprint, tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{10, 10, 10, 10, 10}) {
		t.Errorf("Unexpected counts after increment: %v", counts)
	}

	backfillCounts := []*common.TimeCount{
		{Timestamp: tnow, Count: 11},
		{Timestamp: tnow.Add(-testBucketSize), Count: 9},
		{Timestamp: tnow.Add(-2 * testBucketSize), Count: 9},
		{Timestamp: tnow.Add(-3 * testBucketSize), Count: 12},
		{Timestamp: tnow.Add(-4 * testBucketSize), Count: 11},
	}

	counts.BackfillProperty(pid, backfillCounts)

	if stats := counts.FetchStats(pid, fingerprint, tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{11, 12, 10, 10, 11}) {
		t.Errorf("Unexpected counts after backfill: %v", counts)
	}
}
