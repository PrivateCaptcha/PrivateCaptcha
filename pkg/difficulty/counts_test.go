package difficulty

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	testBucketSize = 5 * time.Minute
	testBuckets    = 5
	testCacheCap   = 10
)

func TestCacheCap(t *testing.T) {
	t.Parallel()

	const cap = 10
	counts := newCounts(testBucketSize, testBuckets, cap)
	fingerprint := common.RandomFingerprint()
	tnow := time.Now()

	for pid := int32(0); pid < cap*2; pid++ {
		counts.Inc(pid, fingerprint, tnow)

		// increment only compacts 1 element max, actual compaction runs in Cleanup()
		if len(counts.stats) > cap {
			t.Errorf("Unexpected cache size after compact: %v", len(counts.stats))
		}
	}
}

func TestCacheCapCleanup(t *testing.T) {
	t.Parallel()

	const cap = 10
	counts := newCounts(testBucketSize, testBuckets, cap)
	fingerprint := common.RandomFingerprint()
	tnow := time.Now()

	for pid := int32(0); pid < cap*2; pid++ {
		counts.Inc(pid, fingerprint, tnow)
	}

	if len(counts.stats) != cap {
		t.Errorf("Unexpected cache size after compact: %v", len(counts.stats))
	}

	maxToCleanup := 2

	deleted := counts.Cleanup(tnow, maxToCleanup)

	if deleted != maxToCleanup {
		t.Errorf("Unexpected deleted count: %v", deleted)
	}

	if len(counts.stats) != (cap - maxToCleanup) {
		t.Errorf("Unexpected cache size after compact: %v (expected %v)", len(counts.stats), cap-maxToCleanup)
	}
}

func TestPropertyStatsInc(t *testing.T) {
	t.Parallel()

	tnow := time.Now()
	pid := int32(12345)
	fingerprint := common.RandomFingerprint()
	counts := newCounts(testBucketSize, testBuckets, testCacheCap)

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
}

func propertyStatsCleanupSuite(properties []int32, t *testing.T) {
	tnow := time.Now()
	fingerprint := common.RandomFingerprint()

	counts := newCounts(testBucketSize, 10, testCacheCap)

	for _, pid := range properties {
		// now we set 1 request per each bucket
		for i := 0; i < 5; i++ {
			if st := counts.Inc(pid, fingerprint, tnow.Add(-time.Duration(i)*testBucketSize).Add(-time.Second)); st != 1 {
				t.Errorf("Unexpected stats: %v (iteration %v)", st, i)
			}
		}

		if stats := counts.FetchStats(pid, fingerprint, tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{1, 1, 1, 1, 1}) {
			t.Errorf("Unexpected counts after increment: %v", stats.Property)
		}
	}

	// for 5 buckets, their intervals are like so:
	// ... | 4 (t-4) | 3 (t-3) | 2 (t-2) | 1 (t-1) | 0 (t)
	// so if we clean from (t-2), it means last 3 buckets will be 0
	if deleted := counts.CleanupEx(tnow, 2, 10); deleted != 0 {
		t.Errorf("Unexpected amount of properties deleted: %v", deleted)
	}

	for _, pid := range properties {
		if stats := counts.FetchStats(pid, fingerprint, tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{0, 0, 0, 1, 1}) {
			t.Errorf("Unexpected counts after cleanup: %v", stats.Property)
		}
	}

	if deleted := counts.CleanupEx(tnow, 0, 10); deleted != len(properties) {
		t.Errorf("Unexpected amount of properties deleted: %v", deleted)
	}
}

func TestPropertyStatsCleanup(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		properties []int32
	}{
		{[]int32{12345}},
		{[]int32{12345, 67890}},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("stats_cleanup_%v", i), func(t *testing.T) {
			propertyStatsCleanupSuite(tc.properties, t)
		})
	}
}

func TestPropertyStatsBackfill(t *testing.T) {
	t.Parallel()

	tnow := time.Now()
	pid := int32(12345)
	fingerprint := common.RandomFingerprint()

	counts := newCounts(testBucketSize, testBuckets, testCacheCap)

	// now we set 10 request per each bucket
	for i := 0; i < 50; i++ {
		counts.Inc(pid, fingerprint, tnow.Add(-time.Duration(i%5)*testBucketSize).Add(-time.Second))
	}

	if stats := counts.FetchStats(pid, fingerprint, tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{10, 10, 10, 10, 10}) {
		t.Errorf("Unexpected counts after increment: %v", stats.Property)
	}

	backfillCounts := []*common.TimeCount{
		{Timestamp: tnow, Count: 11},
		{Timestamp: tnow.Add(-testBucketSize), Count: 9},
		{Timestamp: tnow.Add(-2 * testBucketSize), Count: 9},
		{Timestamp: tnow.Add(-3 * testBucketSize), Count: 12},
		{Timestamp: tnow.Add(-4 * testBucketSize), Count: 11},
	}

	counts.BackfillProperty(pid, backfillCounts, tnow)

	if stats := counts.FetchStats(pid, fingerprint, tnow); !stats.HasProperty || !slices.Equal(stats.Property, []uint32{11, 12, 10, 10, 11}) {
		t.Errorf("Unexpected counts after backfill: %v", stats.Property)
	}
}
