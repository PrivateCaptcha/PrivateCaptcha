package api

import (
	"log/slog"
	"math/rand"
	"testing"
	"time"

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
	levels := difficulty.NewLevelsEx(clickhouse, 200,
		testBucketSize,
		500*time.Millisecond, /*access log*/
		700*time.Millisecond /*backfill*/)
	defer levels.Shutdown()
	tnow := time.Now()

	fingerprints := []string{"qwerty", "abcde"}
	prop1 := &dbgen.Property{
		ID:               123,
		ExternalID:       *randomUUID(),
		UserID:           db.Int(678),
		DifficultyLevel:  dbgen.DifficultyLevelSmall,
		DifficultyGrowth: dbgen.DifficultyGrowthFast,
		CreatedAt:        db.Timestampz(tnow),
		UpdatedAt:        db.Timestampz(tnow),
	}

	var diff uint8
	var stats difficulty.Stats
	buckets := []int{0, 0, 0, 0, 1, 1, 2, 3, 4}

	for i := 0; i < 10000; i++ {
		bucket := buckets[rand.Intn(len(buckets))]
		diff, stats = levels.DifficultyEx(fingerprints[i%2], prop1, time.Now().Add(-time.Duration(bucket)*testBucketSize).UTC())
		if (i+1)%2000 == 0 {
			slog.Debug("Simulating requests", "difficulty", diff, "property", stats.Property, "user", stats.User)
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
	if d := levels.Difficulty(fingerprints[0], prop1); d != difficulty.LevelSmall {
		t.Errorf("Unexpected difficulty after stats reset: %v", d)
	}

	backfilled := false
	var actualDifficulty uint8

	for attempt := 0; attempt < 5; attempt++ {
		// give time to backfill difficulty
		time.Sleep(1 * time.Second)
		actualDifficulty, stats = levels.DifficultyEx(fingerprints[0], prop1, time.Now().UTC())
		if actualDifficulty >= diff {
			backfilled = true
			break
		}

		slog.Debug("Waiting for backfill...", "difficulty", actualDifficulty, "property", stats.Property, "user", stats.User)
	}

	if !backfilled {
		t.Errorf("Difficulty was not backfilled. actual=%v expected=%v", actualDifficulty, diff)
	}
}
