package db

import (
	"context"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestMemoryTimeSeriesPing(t *testing.T) {
	ts := NewMemoryTimeSeries()
	if err := ts.Ping(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestMemoryTimeSeriesRetrievePropertyStatsSince(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	// Use a fixed time aligned to 5 minutes to ensure deterministic bucketing
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	records := []*common.AccessRecord{
		{
			UserID:     1,
			OrgID:      10,
			PropertyID: 100,
			Timestamp:  now,
		},
		{
			UserID:     1,
			OrgID:      10,
			PropertyID: 100,
			Timestamp:  now.Add(1 * time.Minute),
		},
		{
			UserID:     1,
			OrgID:      10,
			PropertyID: 100,
			Timestamp:  now.Add(6 * time.Minute), // Different 5m bucket
		},
		{
			UserID:     2,
			OrgID:      10,
			PropertyID: 100,
			Timestamp:  now,
		},
	}

	err := ts.WriteAccessLogBatch(ctx, records)
	if err != nil {
		t.Fatal(err)
	}

	req := &common.BackfillRequest{
		UserID:     1,
		OrgID:      10,
		PropertyID: 100,
	}
	// Expecting 2 buckets for User 1.
	// Bucket 1: now (truncated to 5m) -> count 2
	// Bucket 2: now+6m (truncated to 5m) -> count 1
	stats, err := ts.RetrievePropertyStatsSince(ctx, req, now.Add(-1*time.Hour))
	if err != nil {
		t.Error(err)
	}

	if actual := len(stats); actual != 2 {
		t.Errorf("RetrievePropertyStatsSince() got %d stats, want 2", actual)
	}

	totalCount := uint32(0)
	for _, s := range stats {
		totalCount += s.Count
	}
	if totalCount != 3 {
		t.Errorf("RetrievePropertyStatsSince() total count = %d, want 3", totalCount)
	}
}

func TestMemoryTimeSeriesRetrieveAccountStats(t *testing.T) {
	// Aggregates by month
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	fixedTime := time.Date(2023, 10, 15, 12, 0, 0, 0, time.UTC)
	records := []*common.AccessRecord{
		{UserID: 1, OrgID: 10, Timestamp: fixedTime},
		{UserID: 1, OrgID: 10, Timestamp: fixedTime.Add(24 * time.Hour)},
		{UserID: 1, OrgID: 20, Timestamp: fixedTime.Add(48 * time.Hour)},
		{UserID: 2, OrgID: 10, Timestamp: fixedTime},
	}
	ts.WriteAccessLogBatch(ctx, records)

	accountStats, err := ts.RetrieveAccountStats(ctx, 1, fixedTime.Add(-24*time.Hour))
	if err != nil {
		t.Error(err)
	}

	if len(accountStats) != 2 {
		t.Errorf("RetrieveAccountStats() got %d stats, want 2", len(accountStats))
	}

	expectedTs := time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC)
	counts := map[int32]uint32{}
	for _, stat := range accountStats {
		if !stat.Timestamp.Equal(expectedTs) {
			t.Errorf("RetrieveAccountStats() timestamp = %v, want %v", stat.Timestamp, expectedTs)
		}
		counts[stat.OrgID] = stat.Count
	}

	if counts[10] != 2 {
		t.Errorf("RetrieveAccountStats() org 10 count = %d, want 2", counts[10])
	}
	if counts[20] != 1 {
		t.Errorf("RetrieveAccountStats() org 20 count = %d, want 1", counts[20])
	}
}

func TestMemoryTimeSeriesVerifyLogsAndStatsByPeriod(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()

	now := time.Now().UTC()

	accessRecords := []*common.AccessRecord{
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-30 * time.Minute)}, // Today
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-2 * time.Hour)},    // Today
	}
	ts.WriteAccessLogBatch(ctx, accessRecords)

	verifyRecords := []*common.VerifyRecord{
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-30 * time.Minute), Status: 1},
	}
	ts.WriteVerifyLogBatch(ctx, verifyRecords)

	stats, err := ts.RetrievePropertyStatsByPeriod(ctx, 1, 1, common.TimePeriodToday)
	if err != nil {
		t.Error(err)
	}

	totalReq := 0
	totalVer := 0
	for _, s := range stats {
		totalReq += s.RequestsCount
		totalVer += s.VerifiesCount
	}

	if totalReq != 2 {
		t.Errorf("RetrievePropertyStatsByPeriod(Today) requests = %d, want 2", totalReq)
	}
	if totalVer != 1 {
		t.Errorf("RetrievePropertyStatsByPeriod(Today) verifies = %d, want 1", totalVer)
	}
}

func TestMemoryTimeSeriesRecentTopProperties(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	now := time.Now().UTC()

	records := []*common.VerifyRecord{
		{PropertyID: 1, Timestamp: now},
		{PropertyID: 1, Timestamp: now},
		{PropertyID: 2, Timestamp: now},
		{PropertyID: 3, Timestamp: now.Add(-48 * time.Hour)}, // Too old
	}

	ts.WriteVerifyLogBatch(ctx, records)

	top, err := ts.RetrieveRecentTopProperties(ctx, 10)
	if err != nil {
		t.Error(err)
	}

	if len(top) != 2 {
		t.Errorf("RetrieveRecentTopProperties() got %d properties, want 2", len(top))
	}

	if top[1] != 2 {
		t.Errorf("Property 1 count = %d, want 2", top[1])
	}
	if top[2] != 1 {
		t.Errorf("Property 2 count = %d, want 1", top[2])
	}
	if _, ok := top[3]; ok {
		t.Errorf("Property 3 should not be in top list (too old)")
	}
}

func TestMemoryTimeSeriesDeletePropertiesData(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()

	// Populate
	ts.WriteAccessLogBatch(ctx, []*common.AccessRecord{
		{UserID: 1, OrgID: 10, PropertyID: 100},
		{UserID: 2, OrgID: 20, PropertyID: 200},
		{UserID: 3, OrgID: 30, PropertyID: 300},
	})
	ts.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{UserID: 1, OrgID: 10, PropertyID: 100},
		{UserID: 2, OrgID: 20, PropertyID: 200},
		{UserID: 3, OrgID: 30, PropertyID: 300},
	})

	if err := ts.DeletePropertiesData(ctx, []int32{100}); err != nil {
		t.Error(err)
	}

	stats, _ := ts.RetrievePropertyStatsSince(ctx, &common.BackfillRequest{UserID: 1, OrgID: 10, PropertyID: 100}, time.Time{})
	if len(stats) != 0 {
		t.Errorf("After DeletePropertiesData, stats count = %d, want 0", len(stats))
	}
}

func TestMemoryTimeSeriesDeleteAccountData(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()

	// Populate
	ts.WriteAccessLogBatch(ctx, []*common.AccessRecord{
		{UserID: 1, OrgID: 10, PropertyID: 100},
		{UserID: 2, OrgID: 20, PropertyID: 200},
		{UserID: 3, OrgID: 30, PropertyID: 300},
	})
	ts.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{UserID: 1, OrgID: 10, PropertyID: 100},
		{UserID: 2, OrgID: 20, PropertyID: 200},
		{UserID: 3, OrgID: 30, PropertyID: 300},
	})

	if err := ts.DeleteOrganizationsData(ctx, []int32{20}); err != nil {
		t.Error(err)
	}
	// Check user 2 (Org 20)
	stats2, _ := ts.RetrieveAccountStats(ctx, 2, time.Time{})
	if len(stats2) != 0 {
		t.Errorf("After DeleteOrganizationsData, stats count = %d, want 0", len(stats2))
	}

	// Delete User 3
	if err := ts.DeleteUsersData(ctx, []int32{3}); err != nil {
		t.Errorf("DeleteUsersData error = %v", err)
	}
	// Check user 3
	stats3, _ := ts.RetrieveAccountStats(ctx, 3, time.Time{})
	if len(stats3) != 0 {
		t.Errorf("After DeleteUsersData, stats count = %d, want 0", len(stats3))
	}
}

func TestMemoryTimeSeriesRetrievePropertyStatsByPeriodAllPeriods(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()

	now := time.Now().UTC()

	// Add records at various times to test different period aggregations
	accessRecords := []*common.AccessRecord{
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-30 * time.Minute)},  // Today
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-2 * time.Hour)},     // Today
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-3 * 24 * time.Hour)}, // This week
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-15 * 24 * time.Hour)}, // This month
	}
	ts.WriteAccessLogBatch(ctx, accessRecords)

	verifyRecords := []*common.VerifyRecord{
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-30 * time.Minute), Status: 1},
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-3 * 24 * time.Hour), Status: 1},
	}
	ts.WriteVerifyLogBatch(ctx, verifyRecords)

	tests := []struct {
		period           common.TimePeriod
		expectedMinStats int
	}{
		{common.TimePeriodToday, 1},
		{common.TimePeriodWeek, 1},
		{common.TimePeriodMonth, 1},
		{common.TimePeriodYear, 1},
	}

	for _, tt := range tests {
		t.Run(tt.period.String(), func(t *testing.T) {
			stats, err := ts.RetrievePropertyStatsByPeriod(ctx, 1, 1, tt.period)
			if err != nil {
				t.Errorf("RetrievePropertyStatsByPeriod(%v) error = %v", tt.period, err)
				return
			}

			if len(stats) < tt.expectedMinStats {
				t.Errorf("RetrievePropertyStatsByPeriod(%v) got %d stats, want at least %d", tt.period, len(stats), tt.expectedMinStats)
			}
		})
	}
}

func TestMemoryTimeSeriesRecentTopPropertiesLimit(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	now := time.Now().UTC()

	// Create verify records for multiple properties
	records := []*common.VerifyRecord{
		{PropertyID: 1, Timestamp: now},
		{PropertyID: 1, Timestamp: now},
		{PropertyID: 1, Timestamp: now},
		{PropertyID: 2, Timestamp: now},
		{PropertyID: 2, Timestamp: now},
		{PropertyID: 3, Timestamp: now},
		{PropertyID: 4, Timestamp: now},
		{PropertyID: 5, Timestamp: now},
	}

	ts.WriteVerifyLogBatch(ctx, records)

	// Test with limit = 2
	top, err := ts.RetrieveRecentTopProperties(ctx, 2)
	if err != nil {
		t.Errorf("RetrieveRecentTopProperties error = %v", err)
		return
	}

	// Should return at most 2 properties (or all if less than limit)
	if len(top) > 2 {
		t.Errorf("RetrieveRecentTopProperties(2) got %d properties, want at most 2", len(top))
	}

	// Test with limit = 10 (more than available)
	topAll, err := ts.RetrieveRecentTopProperties(ctx, 10)
	if err != nil {
		t.Errorf("RetrieveRecentTopProperties error = %v", err)
		return
	}

	if len(topAll) != 5 {
		t.Errorf("RetrieveRecentTopProperties(10) got %d properties, want 5", len(topAll))
	}

	// Verify property 1 has highest count
	if topAll[1] != 3 {
		t.Errorf("Property 1 count = %d, want 3", topAll[1])
	}

	// Test with limit = 0
	topZero, err := ts.RetrieveRecentTopProperties(ctx, 0)
	if err != nil {
		t.Errorf("RetrieveRecentTopProperties(0) error = %v", err)
		return
	}

	if len(topZero) != 0 {
		t.Errorf("RetrieveRecentTopProperties(0) got %d properties, want 0", len(topZero))
	}
}

func TestMemoryTimeSeriesEmptyBatches(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()

	// Test with empty access log batch
	if err := ts.WriteAccessLogBatch(ctx, []*common.AccessRecord{}); err != nil {
		t.Errorf("WriteAccessLogBatch with empty slice error = %v", err)
	}

	// Test with empty verify log batch
	if err := ts.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{}); err != nil {
		t.Errorf("WriteVerifyLogBatch with empty slice error = %v", err)
	}

	// Test delete methods with empty slices
	if err := ts.DeletePropertiesData(ctx, []int32{}); err != nil {
		t.Errorf("DeletePropertiesData with empty slice error = %v", err)
	}

	if err := ts.DeleteOrganizationsData(ctx, []int32{}); err != nil {
		t.Errorf("DeleteOrganizationsData with empty slice error = %v", err)
	}

	if err := ts.DeleteUsersData(ctx, []int32{}); err != nil {
		t.Errorf("DeleteUsersData with empty slice error = %v", err)
	}
}
