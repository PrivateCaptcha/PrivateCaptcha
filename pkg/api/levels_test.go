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
		Level:      dbgen.DifficultyLevelSmall,
		Growth:     dbgen.DifficultyGrowthFast,
		CreatedAt:  db.Timestampz(tnow),
		UpdatedAt:  db.Timestampz(tnow),
	}

	var diff uint8
	var level int64
	buckets := []int{5, 4, 3, 2, 2, 1, 1, 1, 1}
	nanoseconds := testBucketSize.Nanoseconds()
	const iterations = 1000
	diffInterval := int64(nanoseconds / iterations)

	for bucket := range buckets {
		btime := tnow.Add(-time.Duration(bucket) * testBucketSize)

		for i := 0; i < iterations; i++ {
			fingerprint := fingerprints[rand.Intn(len(fingerprints))]
			diff, level = levels.DifficultyEx(fingerprint, prop, btime.Add(time.Duration(int64(i)*diffInterval)*time.Nanosecond))
			if (i+1)%500 == 0 {
				slog.Debug("Simulating requests", "difficulty", diff, "level", level)
			}
		}
	}

	if diff == difficulty.LevelSmall {
		t.Errorf("Difficulty did not grow: %v", diff)
	}

	// we need to wait for the timeout in the ProcessAccessLog() to make sure we have accurate counts
	// and also for cache to expire in BackfillDifficulty()
	time.Sleep(1 * time.Second)

	levels.Reset()

	// now this should cause the backfill request to be fired
	if d := levels.Difficulty(fingerprints[0], prop, time.Now()); d != difficulty.LevelSmall {
		t.Errorf("Unexpected difficulty after stats reset: %v", d)
	}

	backfilled := false
	var actualDifficulty uint8
	var actualLevel int64

	for attempt := 0; attempt < 5; attempt++ {
		// give time to backfill difficulty
		time.Sleep(1 * time.Second)
		actualDifficulty, actualLevel = levels.DifficultyEx(fingerprints[0], prop, time.Now())
		if actualDifficulty >= diff {
			backfilled = true
			break
		}

		slog.Debug("Waiting for backfill...", "difficulty", actualDifficulty, "level", actualLevel)
	}

	if !backfilled {
		t.Errorf("Difficulty was not backfilled. actual=%v expected=%v", actualDifficulty, diff)
	}
}
