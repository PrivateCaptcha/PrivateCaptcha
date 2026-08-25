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
	// Aggregates by month, uses max of request and verify counts
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

	verifyRecords := []*common.VerifyRecord{
		{UserID: 1, OrgID: 10, Timestamp: fixedTime, Status: 1},
		{UserID: 1, OrgID: 10, Timestamp: fixedTime.Add(1 * time.Hour), Status: 1},
		{UserID: 1, OrgID: 10, Timestamp: fixedTime.Add(2 * time.Hour), Status: 1},
	}
	ts.WriteVerifyLogBatch(ctx, verifyRecords)

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

	// org 10: max(2 requests, 3 verifies) = 3
	if counts[10] != 3 {
		t.Errorf("RetrieveAccountStats() org 10 count = %d, want 3", counts[10])
	}
	// org 20: max(1 request, 0 verifies) = 1
	if counts[20] != 1 {
		t.Errorf("RetrieveAccountStats() org 20 count = %d, want 1", counts[20])
	}
}

func TestMemoryTimeSeriesRetrieveAccountStatsByPeriod(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	firstDay := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	secondDay := firstDay.AddDate(0, 0, 1)
	if err := ts.WriteAccessLogBatch(ctx, []*common.AccessRecord{
		{UserID: 1, OrgID: 10, Timestamp: firstDay},
		{UserID: 1, OrgID: 10, Timestamp: secondDay},
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := ts.RetrieveAccountStatsByPeriod(ctx, 1, firstDay.AddDate(0, 0, -1), common.TimePeriodMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("RetrieveAccountStatsByPeriod() got %d stats, want 2", len(stats))
	}
	for index, stat := range stats {
		expectedTimestamp := time.Date(2026, time.August, 23+index, 0, 0, 0, 0, time.UTC)
		if !stat.Timestamp.Equal(expectedTimestamp) {
			t.Errorf("RetrieveAccountStatsByPeriod() timestamp = %v, want %v", stat.Timestamp, expectedTimestamp)
		}
	}
}

func TestMemoryTimeSeriesRetrieveWeeklyAccountReportStats(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	mid := from.AddDate(0, 0, 7)
	to := mid.AddDate(0, 0, 7)

	if err := ts.WriteAccessLogBatch(ctx, []*common.AccessRecord{
		{UserID: 1, Timestamp: from.Add(-time.Second)},
		{UserID: 1, Timestamp: from},
		{UserID: 1, Timestamp: mid.Add(-time.Second)},
		{UserID: 1, Timestamp: mid},
		{UserID: 1, Timestamp: to.Add(-time.Second)},
		{UserID: 1, Timestamp: to},
		{UserID: 4, Timestamp: mid},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ts.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{UserID: 1, Timestamp: from, Status: 0},
		{UserID: 1, Timestamp: mid, Status: 1},
		{UserID: 1, Timestamp: to, Status: 1},
		{UserID: 2, Timestamp: mid, Status: 1},
		{UserID: 4, Timestamp: mid, Status: 1},
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := ts.RetrieveWeeklyAccountReportStats(ctx, []int32{1, 2, 3}, from, mid, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 {
		t.Fatalf("RetrieveWeeklyAccountReportStats() returned %d users, want 3", len(stats))
	}

	user1 := stats[1]
	if user1 == nil {
		t.Fatal("RetrieveWeeklyAccountReportStats() omitted user 1")
	}
	if user1.CurrentRequests != 2 || user1.PrevRequests != 2 {
		t.Errorf("user 1 requests = current %d previous %d, want 2 and 2", user1.CurrentRequests, user1.PrevRequests)
	}
	if user1.CurrentVerifies != 1 || user1.PrevVerifies != 1 {
		t.Errorf("user 1 verifications = current %d previous %d, want 1 and 1", user1.CurrentVerifies, user1.PrevVerifies)
	}

	user2 := stats[2]
	if user2 == nil {
		t.Fatal("RetrieveWeeklyAccountReportStats() omitted verification-only user 2")
	}
	if user2.CurrentRequests != 0 || user2.PrevRequests != 0 || user2.CurrentVerifies != 1 || user2.PrevVerifies != 0 {
		t.Errorf("user 2 stats = %+v, want one current verification only", user2)
	}

	user3 := stats[3]
	if user3 == nil {
		t.Fatal("RetrieveWeeklyAccountReportStats() omitted zero-activity user 3")
	}
	if *user3 != (common.UserReportAccountStats{}) {
		t.Errorf("user 3 stats = %+v, want zero values", user3)
	}
	if _, ok := stats[4]; ok {
		t.Error("RetrieveWeeklyAccountReportStats() included unrequested user 4")
	}
}

func TestMemoryTimeSeriesRetrieveMonthlyAccountReportStats(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	from := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	if err := ts.WriteAccessLogBatch(ctx, []*common.AccessRecord{
		{UserID: 1, Timestamp: mid.Add(-time.Hour)},
		{UserID: 1, Timestamp: mid},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ts.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{UserID: 1, Timestamp: mid.Add(-time.Hour)},
		{UserID: 1, Timestamp: mid},
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := ts.RetrieveMonthlyAccountReportStats(ctx, []int32{1}, from, mid, to)
	if err != nil {
		t.Fatal(err)
	}
	user := stats[1]
	if user == nil {
		t.Fatal("RetrieveMonthlyAccountReportStats() omitted user 1")
	}
	if user.CurrentRequests != 1 || user.PrevRequests != 1 || user.CurrentVerifies != 1 || user.PrevVerifies != 1 {
		t.Errorf("monthly account report stats = %+v, want one count in each period", user)
	}
}

func TestMemoryTimeSeriesRetrievePropertyReportCandidates(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	mid := from.AddDate(0, 0, 7)
	to := mid.AddDate(0, 0, 7)

	accessLogs := make([]*common.AccessRecord, 0)
	verifyLogs := make([]*common.VerifyRecord, 0)
	addRequests := func(propertyID int32, at time.Time, count int) {
		for range count {
			accessLogs = append(accessLogs, &common.AccessRecord{UserID: 1, OrgID: 10, PropertyID: propertyID, Timestamp: at})
		}
	}
	addVerifies := func(propertyID int32, at time.Time, success, failure int) {
		for range success {
			verifyLogs = append(verifyLogs, &common.VerifyRecord{UserID: 1, OrgID: 10, PropertyID: propertyID, Timestamp: at})
		}
		for range failure {
			verifyLogs = append(verifyLogs, &common.VerifyRecord{UserID: 1, OrgID: 10, PropertyID: propertyID, Timestamp: at, Status: 1})
		}
	}

	requestHeavyDay := mid.Add(36 * time.Hour)
	failureHeavyDay := mid.Add(60 * time.Hour)
	nonQualifyingDay := mid.Add(156 * time.Hour)
	addRequests(1, requestHeavyDay, 400)
	addVerifies(1, requestHeavyDay, 100, 0)
	addRequests(1, nonQualifyingDay, 50)
	addVerifies(1, nonQualifyingDay, 100, 0)
	addRequests(1, mid.Add(-time.Hour), 10)
	addRequests(2, failureHeavyDay, 50)
	addVerifies(2, failureHeavyDay, 0, 250)
	addRequests(3, mid.Add(84*time.Hour), 300)
	addVerifies(3, mid.Add(84*time.Hour), 100, 0)
	addRequests(4, mid.Add(108*time.Hour), 99)
	addRequests(5, mid.Add(132*time.Hour), 10)
	addVerifies(5, mid.Add(132*time.Hour), 0, 99)
	addRequests(6, mid.Add(12*time.Hour), 1000)
	addVerifies(6, mid.Add(12*time.Hour), 500, 0)
	addRequests(6, mid.Add(-2*time.Hour), 20)
	addRequests(7, from.Add(time.Hour), 2000)
	addRequests(8, to, 3000)
	addRequests(9, mid.Add(time.Hour), 2000)
	addVerifies(9, mid.Add(time.Hour), 1000, 0)
	addRequests(10, mid.Add(24*time.Hour), 100)
	addVerifies(10, mid.Add(24*time.Hour), 25, 0)
	addRequests(11, mid.Add(48*time.Hour), 100)
	addVerifies(11, mid.Add(48*time.Hour), 20, 0)

	if err := ts.WriteAccessLogBatch(ctx, accessLogs); err != nil {
		t.Fatal(err)
	}
	if err := ts.WriteVerifyLogBatch(ctx, verifyLogs); err != nil {
		t.Fatal(err)
	}

	options := common.UserReportOptions{
		TopPropertiesLimit:                2,
		SecurityEventsLimit:               10,
		SecurityEventsPerPropertyLimit:    10,
		SecurityEventRatioThreshold:       3,
		SecurityEventMinimumDominantCount: 100,
	}
	stats, err := ts.RetrieveWeeklyPropertiesReportStats(ctx, 1, from, mid, to, options)
	if err != nil {
		t.Fatal(err)
	}

	if len(stats.Properties) != 2 {
		t.Fatalf("properties count = %d, want 2", len(stats.Properties))
	}
	if stats.Properties[0].PropertyID != 9 || stats.Properties[0].CurrentRequests != 2000 || stats.Properties[0].PrevRequests != 0 {
		t.Errorf("first property = %+v, want property 9 with 2000 current requests", stats.Properties[0])
	}
	if stats.Properties[1].PropertyID != 6 || stats.Properties[1].CurrentRequests != 1000 || stats.Properties[1].PrevRequests != 20 {
		t.Errorf("second property = %+v, want property 6 with 1000 current and 20 previous requests", stats.Properties[1])
	}

	if len(stats.SecurityEvents) != 4 {
		t.Fatalf("security events count = %d, want 4", len(stats.SecurityEvents))
	}
	requestHeavy := reportProtectionCandidateForTest(t, stats.SecurityEvents, 1)
	if requestHeavy.Requests != 400 || requestHeavy.Verifies != 100 || requestHeavy.FailedVerifies != 0 {
		t.Errorf("request-heavy candidate = %+v, want 400 requests and 100 verifications", requestHeavy)
	}
	if want := time.Date(2026, time.January, 9, 0, 0, 0, 0, time.UTC); !requestHeavy.Timestamp.Equal(want) {
		t.Errorf("request-heavy candidate timestamp = %v, want %v", requestHeavy.Timestamp, want)
	}
	failureHeavy := reportProtectionCandidateForTest(t, stats.SecurityEvents, 2)
	if failureHeavy.Requests != 50 || failureHeavy.Verifies != 250 || failureHeavy.FailedVerifies != 250 {
		t.Errorf("failure-heavy candidate = %+v, want 50 requests and 250 failed verifications", failureHeavy)
	}
	if want := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC); !failureHeavy.Timestamp.Equal(want) {
		t.Errorf("failure-heavy candidate timestamp = %v, want %v", failureHeavy.Timestamp, want)
	}
	higherRatio := reportProtectionCandidateForTest(t, stats.SecurityEvents, 11)
	if higherRatio.Requests != 100 || higherRatio.Verifies != 20 {
		t.Errorf("higher-ratio candidate = %+v, want 100 requests and 20 verifications", higherRatio)
	}
	minimumCount := reportProtectionCandidateForTest(t, stats.SecurityEvents, 10)
	if minimumCount.Requests != 100 || minimumCount.Verifies != 25 {
		t.Errorf("minimum-count candidate = %+v, want 100 requests and 25 verifications", minimumCount)
	}

	limitedOptions := options
	limitedOptions.SecurityEventsLimit = 3
	limited, err := ts.RetrieveWeeklyPropertiesReportStats(ctx, 1, from, mid, to, limitedOptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.SecurityEvents) != 3 {
		t.Fatalf("limited candidates count = %d, want 3", len(limited.SecurityEvents))
	}
	reportProtectionCandidateForTest(t, limited.SecurityEvents, 1)
	reportProtectionCandidateForTest(t, limited.SecurityEvents, 2)
	reportProtectionCandidateForTest(t, limited.SecurityEvents, 11)

	withoutCandidatesOptions := options
	withoutCandidatesOptions.SecurityEventsLimit = 0
	withoutCandidates, err := ts.RetrieveWeeklyPropertiesReportStats(ctx, 1, from, mid, to, withoutCandidatesOptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutCandidates.Properties) != 2 || len(withoutCandidates.SecurityEvents) != 0 {
		t.Errorf("zero-limit result has %d properties and %d candidates, want 2 and 0", len(withoutCandidates.Properties), len(withoutCandidates.SecurityEvents))
	}

	monthly, err := ts.RetrieveMonthlyPropertiesReportStats(ctx, 1, from, mid, to, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(monthly.Properties) != 2 || len(monthly.SecurityEvents) != 4 {
		t.Errorf("monthly result has %d properties and %d candidates, want 2 and 4", len(monthly.Properties), len(monthly.SecurityEvents))
	}
}

func TestMemoryTimeSeriesPropertyReportCandidatesLimitPerProperty(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	mid := from.AddDate(0, 0, 7)
	to := mid.AddDate(0, 0, 7)

	accessLogs := make([]*common.AccessRecord, 0)
	verifyLogs := make([]*common.VerifyRecord, 0)
	addEvent := func(propertyID int32, day int, requests, verifies int) {
		at := mid.AddDate(0, 0, day)
		for range requests {
			accessLogs = append(accessLogs, &common.AccessRecord{UserID: 1, OrgID: 10, PropertyID: propertyID, Timestamp: at})
		}
		for range verifies {
			verifyLogs = append(verifyLogs, &common.VerifyRecord{UserID: 1, OrgID: 10, PropertyID: propertyID, Timestamp: at})
		}
	}

	addEvent(1, 1, 500, 100)
	addEvent(1, 2, 400, 80)
	addEvent(1, 3, 300, 60)
	addEvent(2, 4, 250, 50)
	addEvent(2, 5, 200, 40)
	if err := ts.WriteAccessLogBatch(ctx, accessLogs); err != nil {
		t.Fatal(err)
	}
	if err := ts.WriteVerifyLogBatch(ctx, verifyLogs); err != nil {
		t.Fatal(err)
	}

	stats, err := ts.RetrieveWeeklyPropertiesReportStats(ctx, 1, from, mid, to, common.UserReportOptions{
		TopPropertiesLimit:                2,
		SecurityEventsLimit:               4,
		SecurityEventsPerPropertyLimit:    2,
		SecurityEventRatioThreshold:       3,
		SecurityEventMinimumDominantCount: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.SecurityEvents) != 4 {
		t.Fatalf("security events count = %d, want 4", len(stats.SecurityEvents))
	}

	countsByProperty := make(map[int32]int)
	requestsByProperty := make(map[int32]map[uint64]bool)
	for _, candidate := range stats.SecurityEvents {
		countsByProperty[candidate.PropertyID]++
		if requestsByProperty[candidate.PropertyID] == nil {
			requestsByProperty[candidate.PropertyID] = make(map[uint64]bool)
		}
		requestsByProperty[candidate.PropertyID][candidate.Requests] = true
	}
	if countsByProperty[1] != 2 || countsByProperty[2] != 2 {
		t.Errorf("candidate counts by property = %v, want map[1:2 2:2]", countsByProperty)
	}
	if !requestsByProperty[1][500] || !requestsByProperty[1][400] || requestsByProperty[1][300] {
		t.Errorf("property 1 request counts = %v, want only 500 and 400", requestsByProperty[1])
	}
	if !requestsByProperty[2][250] || !requestsByProperty[2][200] {
		t.Errorf("property 2 request counts = %v, want 250 and 200", requestsByProperty[2])
	}
}

func TestMemoryTimeSeriesPropertyReportCandidatesUseOnlyQualifyingDominantCounts(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	mid := from.AddDate(0, 0, 7)
	to := mid.AddDate(0, 0, 7)
	day := mid.AddDate(0, 0, 1)

	accessLogs := make([]*common.AccessRecord, 0, 190)
	for range 100 {
		accessLogs = append(accessLogs, &common.AccessRecord{UserID: 1, OrgID: 10, PropertyID: 1, Timestamp: day})
	}
	for range 90 {
		accessLogs = append(accessLogs, &common.AccessRecord{UserID: 1, OrgID: 10, PropertyID: 2, Timestamp: day})
	}
	verifyLogs := make([]*common.VerifyRecord, 0, 300)
	for i := range 200 {
		status := int8(0)
		if i < 80 {
			status = 1
		}
		verifyLogs = append(verifyLogs, &common.VerifyRecord{UserID: 1, OrgID: 10, PropertyID: 1, Timestamp: day, Status: status})
	}
	for range 100 {
		verifyLogs = append(verifyLogs, &common.VerifyRecord{UserID: 1, OrgID: 10, PropertyID: 2, Timestamp: day})
	}
	if err := ts.WriteAccessLogBatch(ctx, accessLogs); err != nil {
		t.Fatal(err)
	}
	if err := ts.WriteVerifyLogBatch(ctx, verifyLogs); err != nil {
		t.Fatal(err)
	}

	stats, err := ts.RetrieveWeeklyPropertiesReportStats(ctx, 1, from, mid, to, common.UserReportOptions{
		TopPropertiesLimit:                2,
		SecurityEventsLimit:               1,
		SecurityEventsPerPropertyLimit:    1,
		SecurityEventRatioThreshold:       0.75,
		SecurityEventMinimumDominantCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.SecurityEvents) != 1 {
		t.Fatalf("security events count = %d, want 1", len(stats.SecurityEvents))
	}
	if candidate := stats.SecurityEvents[0]; candidate.PropertyID != 2 || candidate.Requests != 90 || candidate.Verifies != 100 {
		t.Errorf("candidate = %+v, want property 2 selected by qualifying dominant count 90", candidate)
	}
}

func TestMemoryTimeSeriesPropertyReportCandidatesUseOneForZeroDenominators(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	mid := from.AddDate(0, 0, 7)
	to := mid.AddDate(0, 0, 7)
	day := mid.AddDate(0, 0, 1)

	accessLogs := make([]*common.AccessRecord, 100)
	verifyLogs := make([]*common.VerifyRecord, 100)
	for i := range 100 {
		accessLogs[i] = &common.AccessRecord{UserID: 1, OrgID: 10, PropertyID: 1, Timestamp: day}
		verifyLogs[i] = &common.VerifyRecord{UserID: 1, OrgID: 10, PropertyID: 2, Timestamp: day, Status: 1}
	}
	if err := ts.WriteAccessLogBatch(ctx, accessLogs); err != nil {
		t.Fatal(err)
	}
	if err := ts.WriteVerifyLogBatch(ctx, verifyLogs); err != nil {
		t.Fatal(err)
	}

	stats, err := ts.RetrieveWeeklyPropertiesReportStats(ctx, 1, from, mid, to, common.UserReportOptions{
		TopPropertiesLimit:                2,
		SecurityEventsLimit:               2,
		SecurityEventsPerPropertyLimit:    2,
		SecurityEventRatioThreshold:       3,
		SecurityEventMinimumDominantCount: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.SecurityEvents) != 2 {
		t.Fatalf("security events count = %d, want 2", len(stats.SecurityEvents))
	}
	requestHeavy := reportProtectionCandidateForTest(t, stats.SecurityEvents, 1)
	if requestHeavy.Requests != 100 || requestHeavy.Verifies != 0 {
		t.Errorf("request-heavy candidate = %+v, want 100 requests and zero verifications", requestHeavy)
	}
	failureHeavy := reportProtectionCandidateForTest(t, stats.SecurityEvents, 2)
	if failureHeavy.Requests != 0 || failureHeavy.Verifies != 100 || failureHeavy.FailedVerifies != 100 {
		t.Errorf("failure-heavy candidate = %+v, want 100 failed verifications and zero requests", failureHeavy)
	}
}

func reportProtectionCandidateForTest(t *testing.T, candidates []*common.UserReportSecurityEvent, propertyID int32) *common.UserReportSecurityEvent {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.PropertyID == propertyID {
			return candidate
		}
	}
	t.Fatalf("protection candidate for property %d not found in %+v", propertyID, candidates)
	return nil
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
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-30 * time.Minute)},    // Today
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-2 * time.Hour)},       // Today
		{OrgID: 1, PropertyID: 1, Timestamp: now.Add(-3 * 24 * time.Hour)},  // This week
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

func TestMemoryTimeSeriesRetrieveFormStatsByPeriod(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	now := time.Now().UTC()

	if err := ts.WriteFormSubmitBatch(ctx, []*common.FormSubmitRecord{
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now.Add(-30 * time.Minute), Status: 0},
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now.Add(-20 * time.Minute), Status: 0},
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now.Add(-10 * time.Minute), Status: 1},
		{UserID: 1, OrgID: 10, FormID: 2000, Timestamp: now.Add(-10 * time.Minute), Status: 0},
	}); err != nil {
		t.Fatal(err)
	}

	stats, err := ts.RetrieveFormStatsByPeriod(ctx, 10, 1000, common.TimePeriodToday)
	if err != nil {
		t.Fatal(err)
	}

	successCount := 0
	failureCount := 0
	for _, stat := range stats {
		successCount += stat.SuccessCount
		failureCount += stat.FailureCount
	}

	if successCount != 2 {
		t.Errorf("RetrieveFormStatsByPeriod() success count = %d, want 2", successCount)
	}
	if failureCount != 1 {
		t.Errorf("RetrieveFormStatsByPeriod() failure count = %d, want 1", failureCount)
	}
}

func TestMemoryTimeSeriesRetrieveFailingForms(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Hour)

	if err := ts.WriteFormSubmitBatch(ctx, []*common.FormSubmitRecord{
		// Qualifies: latest three non-empty hourly records are failures, even with older success.
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now.Add(-5 * time.Hour), Status: 0},
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now.Add(-3 * time.Hour), Status: 1},
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now.Add(-2 * time.Hour), Status: 1},
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now.Add(-1 * time.Hour), Status: 1},
		// Does not qualify: latest three include a success.
		{UserID: 1, OrgID: 10, FormID: 2000, Timestamp: now.Add(-3 * time.Hour), Status: 1},
		{UserID: 1, OrgID: 10, FormID: 2000, Timestamp: now.Add(-2 * time.Hour), Status: 0},
		{UserID: 1, OrgID: 10, FormID: 2000, Timestamp: now.Add(-1 * time.Hour), Status: 1},
		// Does not qualify: fewer than threshold records.
		{UserID: 1, OrgID: 10, FormID: 3000, Timestamp: now.Add(-2 * time.Hour), Status: 1},
		{UserID: 1, OrgID: 10, FormID: 3000, Timestamp: now.Add(-1 * time.Hour), Status: 1},
		// Qualifies, used to verify max result limit.
		{UserID: 1, OrgID: 10, FormID: 4000, Timestamp: now.Add(-3 * time.Hour), Status: 1},
		{UserID: 1, OrgID: 10, FormID: 4000, Timestamp: now.Add(-2 * time.Hour), Status: 1},
		{UserID: 1, OrgID: 10, FormID: 4000, Timestamp: now.Add(-1 * time.Hour), Status: 1},
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := ts.RetrieveFailingForms(ctx, 3, 1)
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 1 {
		t.Fatalf("RetrieveFailingForms() returned %d candidates, want 1", len(candidates))
	}
	if candidates[0].FormID != 1000 {
		t.Errorf("RetrieveFailingForms() form ID = %d, want 1000", candidates[0].FormID)
	}
	if candidates[0].FailureCount != 3 {
		t.Errorf("RetrieveFailingForms() failure count = %d, want 3", candidates[0].FailureCount)
	}
}

func TestMemoryTimeSeriesDeleteFormsData(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	now := time.Now().UTC()

	if err := ts.WriteFormSubmitBatch(ctx, []*common.FormSubmitRecord{
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now, Status: 0},
		{UserID: 1, OrgID: 10, FormID: 2000, Timestamp: now, Status: 1},
	}); err != nil {
		t.Fatal(err)
	}

	if err := ts.DeleteFormsData(ctx, []int32{1000}); err != nil {
		t.Fatal(err)
	}

	stats, err := ts.RetrieveFormStatsByPeriod(ctx, 10, 1000, common.TimePeriodToday)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 0 {
		t.Fatalf("DeleteFormsData() left %d stats for deleted form, want 0", len(stats))
	}

	stats, err = ts.RetrieveFormStatsByPeriod(ctx, 10, 2000, common.TimePeriodToday)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Fatal("DeleteFormsData() deleted unrelated form stats")
	}
}

func TestMemoryTimeSeriesDeletesFormDataByOwner(t *testing.T) {
	ts := NewMemoryTimeSeries()
	ctx := context.Background()
	now := time.Now().UTC()

	if err := ts.WriteFormSubmitBatch(ctx, []*common.FormSubmitRecord{
		{UserID: 1, OrgID: 10, FormID: 1000, Timestamp: now, Status: 0},
		{UserID: 2, OrgID: 20, FormID: 2000, Timestamp: now, Status: 1},
		{UserID: 3, OrgID: 30, FormID: 3000, Timestamp: now, Status: 0},
	}); err != nil {
		t.Fatal(err)
	}

	if err := ts.DeleteFormsData(ctx, []int32{1000}); err != nil {
		t.Fatal(err)
	}
	if stats, err := ts.RetrieveFormStatsByPeriod(ctx, 10, 1000, common.TimePeriodToday); err != nil || len(stats) != 0 {
		t.Fatalf("DeletePropertiesData() form stats len = %d, err = %v, want 0 nil", len(stats), err)
	}

	if err := ts.DeleteOrganizationsData(ctx, []int32{20}); err != nil {
		t.Fatal(err)
	}
	if stats, err := ts.RetrieveFormStatsByPeriod(ctx, 20, 2000, common.TimePeriodToday); err != nil || len(stats) != 0 {
		t.Fatalf("DeleteOrganizationsData() form stats len = %d, err = %v, want 0 nil", len(stats), err)
	}

	if err := ts.DeleteUsersData(ctx, []int32{3}); err != nil {
		t.Fatal(err)
	}
	if stats, err := ts.RetrieveFormStatsByPeriod(ctx, 30, 3000, common.TimePeriodToday); err != nil || len(stats) != 0 {
		t.Fatalf("DeleteUsersData() form stats len = %d, err = %v, want 0 nil", len(stats), err)
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
