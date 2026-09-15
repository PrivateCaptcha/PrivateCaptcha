package difficulty

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

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
