package difficulty

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
)

func TestDifficultyFormula(t *testing.T) {
	testCases := []struct {
		// note that user level is kind of "direct" user level
		userLevel uint32
		// and property level is the "anomaly" level
		propertyLevel uint32
		minDifficulty float64
		growthLevel   dbgen.DifficultyGrowth
		expected      uint8
	}{
		{0, 0, 10, dbgen.DifficultyGrowthMedium, 10},
		{0, 0, 100, dbgen.DifficultyGrowthMedium, 100},
		{0, 1, 100, dbgen.DifficultyGrowthMedium, 100},
		{0, 10, 100, dbgen.DifficultyGrowthMedium, 101},
		{1, 0, 100, dbgen.DifficultyGrowthMedium, 101},
		{0, 100, 100, dbgen.DifficultyGrowthMedium, 105},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("difficulty_%v", i), func(t *testing.T) {
			a := NewDifficultyAlgorithm(5 * time.Minute)
			growth := growthMultiplier(tc.growthLevel)
			actual := a.requestsToDifficulty(tc.userLevel, tc.propertyLevel, 1.0, 1.0, tc.minDifficulty, growth)
			if actual != tc.expected {
				t.Errorf("Actual difficulty (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

func TestDifficultyFormulaIncreasesForVerificationRateDeviation(t *testing.T) {
	a := NewDifficultyAlgorithm(5 * time.Minute)

	difficulty := func(rate float64) uint8 {
		return a.requestsToDifficulty(0, 0, 1.0, rate, 100, 1.0)
	}

	neutral := difficulty(1.0)
	if neutral != 100 {
		t.Fatalf("Neutral verification rate difficulty = %d, want 100", neutral)
	}

	if slight, large := difficulty(1.2), difficulty(3.0); slight <= neutral || large <= slight {
		t.Errorf("Increasing high-side deviation difficulties = %d, %d, %d; want strictly increasing", neutral, slight, large)
	}
	if slight, large := difficulty(0.8), difficulty(0.3); slight <= neutral || large <= slight {
		t.Errorf("Increasing low-side deviation difficulties = %d, %d, %d; want strictly increasing", neutral, slight, large)
	}
	if high, low := difficulty(3.0), difficulty(1.0/3.0); high != low {
		t.Errorf("Reciprocal verification rates produced difficulties %d and %d, want equal", high, low)
	}
}

func TestDifficultyFormulaBoundsOneSidedVerificationRates(t *testing.T) {
	a := NewDifficultyAlgorithm(5 * time.Minute)

	for _, rate := range []float64{0, math.Inf(1)} {
		actual := a.requestsToDifficulty(0, 0, 1.0, rate, 100, 1.0)
		if actual <= 100 || actual >= 255 {
			t.Errorf("Difficulty for verification rate %v = %d, want bounded increase", rate, actual)
		}
	}
}

func TestDifficultyUsesVerificationRate(t *testing.T) {
	a := NewDifficultyAlgorithm(5 * time.Minute)
	property := NewStubProperty(1, true, 1, 1, 100, dbgen.DifficultyGrowthMedium)
	propertyData := &leakybucket.AddResult{LeakRate: 1.0}
	userData := &leakybucket.AddResult{}

	neutral := a.Difficulty(propertyData, userData, property, 1.0)
	imbalanced := a.Difficulty(propertyData, userData, property, 3.0)
	if imbalanced <= neutral {
		t.Errorf("Imbalanced verification difficulty = %d, want greater than neutral difficulty %d", imbalanced, neutral)
	}
}

type capturingAlgorithm struct {
	verificationRate float64
}

func (a *capturingAlgorithm) Difficulty(_ *leakybucket.AddResult, _ *leakybucket.AddResult, _ Property, verificationRate float64) uint8 {
	a.verificationRate = verificationRate
	return 100
}

func TestLevelsForwardsVerificationRate(t *testing.T) {
	algorithm := &capturingAlgorithm{}
	levels := NewLevelsEx(nil, algorithm, 1, 5*time.Minute)
	property := NewStubProperty(1, true, 1, 1, 100, dbgen.DifficultyGrowthMedium)

	levels.DifficultyEx(t.Context(), 1, property, time.Now(), 3.0)

	if algorithm.verificationRate != 3.0 {
		t.Errorf("Algorithm verification rate = %v, want 3", algorithm.verificationRate)
	}
}

type backfillTimeSeries struct {
	common.TimeSeriesStore
	counts []*common.TimeCount
}

func (s *backfillTimeSeries) RetrievePropertyStatsSince(context.Context, *common.BackfillRequest, time.Time) ([]*common.TimeCount, error) {
	return s.counts, nil
}

func TestBackfillDifficultyTrainsLeakRate(t *testing.T) {
	const (
		propertyID = int32(123)
		intervals  = 12
	)
	interval := 5 * time.Minute
	tnow := time.Now().UTC()

	for _, requests := range []uint32{50, 100, 1_000} {
		t.Run(fmt.Sprintf("%dRequestsPerInterval", requests), func(t *testing.T) {
			counts := make([]*common.TimeCount, intervals+1)
			for i := range counts {
				counts[i] = &common.TimeCount{
					Timestamp: tnow.Add(-time.Duration(len(counts)-i) * interval),
					Count:     requests,
				}
			}
			counts[0].Count = requests * 10

			levels := NewLevels(&backfillTimeSeries{counts: counts}, 1, interval)
			levels.propertyBuckets.Add(propertyID, 1, tnow)
			levels.backfillChan <- &common.BackfillRequest{PropertyID: propertyID}
			close(levels.backfillChan)

			levels.backfillDifficulty(t.Context(), time.Hour)

			liveTime := time.Now().UTC()
			result := levels.propertyBuckets.Add(propertyID, 1, liveTime)
			if math.Abs(result.LeakRate-float64(requests)) > 1e-6 {
				t.Errorf("LeakRate after backfill = %v, want %v", result.LeakRate, requests)
			}

			wantLevel := uint32(2 + intervals*requests)
			if result.CurrLevel != wantLevel {
				t.Errorf("CurrLevel after backfill = %v, want %v", result.CurrLevel, wantLevel)
			}

			liveRequests := requests / 2
			levels.propertyBuckets.Add(propertyID, liveRequests-2, liveTime)
			nextInterval := liveTime.Add(interval + time.Second)
			levels.propertyBuckets.Add(propertyID, 1, nextInterval)
			result = levels.propertyBuckets.Add(propertyID, 1, nextInterval)
			wantLeakRate := float64(intervals*requests+liveRequests) / float64(intervals+1)
			if math.Abs(result.LeakRate-wantLeakRate) > 1e-6 {
				t.Errorf("LeakRate after the next interval = %v, want %v", result.LeakRate, wantLeakRate)
			}
		})
	}
}

func TestBackfillDifficultyCountsEmptyIntervals(t *testing.T) {
	const (
		propertyID = int32(123)
		requests   = uint32(1_200)
	)
	interval := 5 * time.Minute
	tnow := time.Now().UTC()
	levels := NewLevels(&backfillTimeSeries{counts: []*common.TimeCount{{
		Timestamp: tnow.Add(-interval),
		Count:     requests,
	}}}, 1, interval)
	levels.propertyBuckets.Add(propertyID, 1, tnow)
	levels.backfillChan <- &common.BackfillRequest{PropertyID: propertyID}
	close(levels.backfillChan)

	levels.backfillDifficulty(t.Context(), time.Hour)

	result := levels.propertyBuckets.Add(propertyID, 1, time.Now().UTC())
	wantLeakRate := float64(requests) / propertyBackfillIntervalCount
	if math.Abs(result.LeakRate-wantLeakRate) > 1e-6 {
		t.Errorf("LeakRate after sparse backfill = %v, want %v", result.LeakRate, wantLeakRate)
	}
}
