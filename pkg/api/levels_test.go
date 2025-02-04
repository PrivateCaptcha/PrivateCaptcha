package api

import (
	"log/slog"
	"math/rand"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
)

const (
	testBucketSize = 5 * time.Minute
)

// this test is in api package due to the need to connect to clickhouse
func TestBackfillLevels(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// minutes per bucket
	levels := difficulty.NewLevelsEx(timeSeries, 200,
		testBucketSize,
		500*time.Millisecond, /*access log*/
		700*time.Millisecond /*backfill*/)
	defer levels.Shutdown()
	tnow := time.Now()
	userID := int32(123)

	fingerprints := []common.TFingerprint{common.RandomFingerprint(), common.RandomFingerprint(), common.RandomFingerprint()}
	prop := &dbgen.Property{
		ID:         123,
		ExternalID: *randomUUID(),
		OrgOwnerID: db.Int(userID),
		OrgID:      db.Int(678),
		Level:      db.Int2(int16(common.DifficultyLevelSmall)),
		Growth:     dbgen.DifficultyGrowthFast,
		CreatedAt:  db.Timestampz(tnow),
		UpdatedAt:  db.Timestampz(tnow),
	}

	var diff uint8
	var level leakybucket.TLevel
	// NOTE: for the buckets that are "the same", we will not update {leakRate} on the 2+ go as
	// from the bucket's perspective following events will be "in the past" (time will be advanced on the 1st go)
	// and for the past events we only increment the level, as the leak "has been accounted for"
	buckets := []int{5, 4, 3, 2, 2, 1, 1, 1, 1}
	nanoseconds := testBucketSize.Nanoseconds()
	const iterations = 1000
	diffInterval := time.Duration(nanoseconds / iterations)

	for _, bucket := range buckets {
		btime := tnow.Add(-time.Duration(bucket) * testBucketSize)

		for i := 0; i < iterations; i++ {
			fingerprint := fingerprints[rand.Intn(len(fingerprints))]
			t := btime.Add(time.Duration(i) * diffInterval)
			diff, level = levels.DifficultyEx(fingerprint, prop, t)
			if (i+1)%250 == 0 {
				slog.Debug("Simulating requests", "difficulty", diff, "level", level, "eventTime", t, "i", i, "bucket", bucket)
			}
		}
	}

	if diff == uint8(common.DifficultyLevelSmall) {
		t.Errorf("Difficulty did not grow: %v", diff)
	}

	// we need to wait for the timeout in the ProcessAccessLog() to make sure we have accurate counts
	// and also for cache to expire in BackfillDifficulty()
	time.Sleep(1 * time.Second)

	levels.ResetProperties()

	// now this should cause the backfill request to be fired
	if d, l := levels.DifficultyEx(fingerprints[0], prop, tnow); d != uint8(common.DifficultyLevelSmall) {
		t.Errorf("Unexpected difficulty after stats reset: %v (level %v)", d, l)
	}

	backfilled := false
	var actualDifficulty uint8
	var actualLevel leakybucket.TLevel

	for attempt := 0; attempt < 5; attempt++ {
		// give time to backfill difficulty
		time.Sleep(1 * time.Second)
		fingerprint := fingerprints[rand.Intn(len(fingerprints))]
		actualDifficulty, actualLevel = levels.DifficultyEx(fingerprint, prop, tnow)
		if (actualDifficulty >= diff) && (actualDifficulty-diff < 5) {
			backfilled = true
			break
		}

		slog.Debug("Waiting for backfill...", "difficulty", actualDifficulty, "level", actualLevel)
	}

	slog.Debug("Backfill waiting finished", "difficulty", actualDifficulty, "level", actualLevel)

	if !backfilled {
		t.Errorf("Difficulty was not backfilled. actual=%v expected=%v", actualDifficulty, diff)
	}
}
