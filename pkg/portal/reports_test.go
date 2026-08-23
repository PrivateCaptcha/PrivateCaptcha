package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

const reportPortalURL = "https://portal.privatecaptcha.test"

func newScheduleReportsJob(usersLimit int32) *maintenance.ScheduleReportsJob {
	return &maintenance.ScheduleReportsJob{
		Store:       store,
		TimeSeries:  timeSeries,
		PlanService: server.PlanService,
		PortalURL:   reportPortalURL,
		IDHasher:    server.IDHasher,
		Stage:       common.StageTest,
		UsersLimit:  usersLimit,
	}
}

func newWeeklyReport(ts common.TimeSeriesStore) *maintenance.ScheduleReportsJob {
	return &maintenance.ScheduleReportsJob{
		Store:      store,
		TimeSeries: ts,
		PortalURL:  reportPortalURL,
		IDHasher:   server.IDHasher,
	}
}

func newMonthlyReport(ts common.TimeSeriesStore) *maintenance.ScheduleReportsJob {
	return &maintenance.ScheduleReportsJob{
		Store:      store,
		TimeSeries: ts,
		PortalURL:  reportPortalURL,
		IDHasher:   server.IDHasher,
	}
}

func TestScheduleWeeklyReport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}
	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "weekly-schedule.reports-test.org"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("failed to create weekly report form: %v", err)
	}

	_, _, err = store.Impl().UpsertUserSettings(ctx, &dbgen.UpsertUserSettingsParams{
		UserID:       user.ID,
		WeeklyReport: true,
	})
	if err != nil {
		t.Fatalf("failed to upsert user settings: %v", err)
	}

	job := newScheduleReportsJob(50)

	// Monday so weekly reports trigger. Keep it recent enough for ClickHouse TTLs.
	tnow := recentWeeklyReportTime(time.Now().UTC())
	seedFormSubmitLogsToStore(t, timeSeries, user.ID, form.ID, org.ID, tnow.AddDate(0, 0, -7), 0, 20)
	seedFormSubmitLogsToStore(t, timeSeries, user.ID, form.ID, org.ID, tnow.AddDate(0, 0, -7), 1, 5)
	seedFormSubmitLogsToStore(t, timeSeries, user.ID, form.ID, org.ID, tnow.AddDate(0, 0, -14), 0, 10)
	seedFormSubmitLogsToStore(t, timeSeries, user.ID, form.ID, org.ID, tnow.AddDate(0, 0, -14), 1, 2)

	params := &maintenance.ScheduleReportsParams{
		UsersLimit: 50,
		UserID:     user.ID,
		Weekly:     true,
		Monthly:    false,
	}

	if err := job.RunOnceAt(ctx, params, tnow); err != nil {
		t.Fatalf("RunOnceAt failed: %v", err)
	}

	notifications, err := store.Impl().RetrievePendingUserNotifications(ctx, tnow.Add(-1*time.Minute), 100, 5)
	if err != nil {
		t.Fatalf("failed to retrieve pending notifications: %v", err)
	}

	year, week := tnow.ISOWeek()
	expectedRef := fmt.Sprintf("%s%d/%d/%d", maintenance.WeeklyReferencePrefix, user.ID, year, week)

	payload := usageReportPayload(t, notifications, user.ID, expectedRef)
	if payload.TotalFormSubmissions != 25 {
		t.Errorf("expected TotalFormSubmissions=25, got %d", payload.TotalFormSubmissions)
	}
	if payload.TotalFormErrors != 5 {
		t.Errorf("expected TotalFormErrors=5, got %d", payload.TotalFormErrors)
	}
	if len(payload.TopForms) != 1 {
		t.Fatalf("expected 1 TopForms entry, got %d", len(payload.TopForms))
	}
	if payload.TopForms[0].Name != form.Name {
		t.Errorf("expected top form name=%q, got %q", form.Name, payload.TopForms[0].Name)
	}
}

func TestScheduleMonthlyReport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}
	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "monthly-schedule.reports-test.org"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("failed to create monthly report form: %v", err)
	}

	_, _, err = store.Impl().UpsertUserSettings(ctx, &dbgen.UpsertUserSettingsParams{
		UserID:        user.ID,
		MonthlyReport: true,
	})
	if err != nil {
		t.Fatalf("failed to upsert user settings: %v", err)
	}

	job := newScheduleReportsJob(50)

	// 1st of month so monthly reports trigger. Keep it recent enough for ClickHouse TTLs.
	tnow := recentMonthlyReportTime(time.Now().UTC())
	seedFormSubmitLogsToStore(t, timeSeries, user.ID, form.ID, org.ID, tnow.AddDate(0, -1, 0), 0, 40)
	seedFormSubmitLogsToStore(t, timeSeries, user.ID, form.ID, org.ID, tnow.AddDate(0, -1, 0), 1, 10)
	seedFormSubmitLogsToStore(t, timeSeries, user.ID, form.ID, org.ID, tnow.AddDate(0, -2, 0), 0, 20)
	seedFormSubmitLogsToStore(t, timeSeries, user.ID, form.ID, org.ID, tnow.AddDate(0, -2, 0), 1, 5)

	params := &maintenance.ScheduleReportsParams{
		UsersLimit: 50,
		UserID:     user.ID,
		Weekly:     false,
		Monthly:    true,
	}

	if err := job.RunOnceAt(ctx, params, tnow); err != nil {
		t.Fatalf("RunOnceAt failed: %v", err)
	}

	notifications, err := store.Impl().RetrievePendingUserNotifications(ctx, tnow.Add(-1*time.Minute), 100, 5)
	if err != nil {
		t.Fatalf("failed to retrieve pending notifications: %v", err)
	}

	expectedRef := fmt.Sprintf("%s%d/%d/%d", maintenance.MonthlyReferencePrefix, user.ID, tnow.Year(), int(tnow.Month()))

	payload := usageReportPayload(t, notifications, user.ID, expectedRef)
	if payload.TotalFormSubmissions != 50 {
		t.Errorf("expected TotalFormSubmissions=50, got %d", payload.TotalFormSubmissions)
	}
	if payload.TotalFormErrors != 10 {
		t.Errorf("expected TotalFormErrors=10, got %d", payload.TotalFormErrors)
	}
	if len(payload.TopForms) != 1 {
		t.Fatalf("expected 1 TopForms entry, got %d", len(payload.TopForms))
	}
	if payload.TopForms[0].Name != form.Name {
		t.Errorf("expected top form name=%q, got %q", form.Name, payload.TopForms[0].Name)
	}
}

func TestScheduleReportsJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	accounts := make([]struct {
		userID     int32
		orgID      int32
		propertyID int32
	}, 2)
	for i := range accounts {
		user, org, err := db_tests.CreateNewAccountForTest(ctx, store, fmt.Sprintf("%s-%d", t.Name(), i), testPlan)
		if err != nil {
			t.Fatalf("failed to create account %d: %v", i, err)
		}
		property, err := db_tests.CreatePropertyForOrg(ctx, store, org)
		if err != nil {
			t.Fatalf("failed to create property %d: %v", i, err)
		}
		if _, _, err := store.Impl().UpsertUserSettings(ctx, &dbgen.UpsertUserSettingsParams{UserID: user.ID, WeeklyReport: true, MonthlyReport: true}); err != nil {
			t.Fatalf("failed to enable reports for account %d: %v", i, err)
		}
		accounts[i].userID = user.ID
		accounts[i].orgID = org.ID
		accounts[i].propertyID = property.ID
	}

	// 2025-09-01 is a Monday and the 1st of the month, so both weekly and monthly trigger
	tnow := time.Date(2025, 9, 1, 10, 0, 0, 0, time.UTC)
	ts := db.NewMemoryTimeSeries()
	for i, account := range accounts {
		factor := i + 1
		seedTimeSeries(t, ts, account.userID, account.propertyID, account.orgID, tnow.AddDate(0, 0, -2), 2*factor)
		seedTimeSeries(t, ts, account.userID, account.propertyID, account.orgID, tnow.AddDate(0, 0, -9), factor)
		seedTimeSeries(t, ts, account.userID, account.propertyID, account.orgID, time.Date(2025, time.July, 15, 12, 0, 0, 0, time.UTC), 3*factor)
		seedVerifyLogs(t, ts, account.userID, account.propertyID, account.orgID, tnow.AddDate(0, 0, -2), factor)
		seedVerifyLogs(t, ts, account.userID, account.propertyID, account.orgID, tnow.AddDate(0, 0, -9), factor)
		seedVerifyLogs(t, ts, account.userID, account.propertyID, account.orgID, time.Date(2025, time.July, 15, 12, 0, 0, 0, time.UTC), factor)
	}
	job := newScheduleReportsJob(1)
	job.TimeSeries = ts

	if err := job.RunOnceAt(ctx, nil, tnow); err != nil {
		t.Fatalf("RunOnceAt failed: %v", err)
	}

	notifications, err := store.Impl().RetrievePendingUserNotifications(ctx, tnow.Add(-1*time.Minute), 100, 5)
	if err != nil {
		t.Fatalf("failed to retrieve pending notifications: %v", err)
	}

	year, week := tnow.ISOWeek()
	for i, account := range accounts {
		factor := uint64(i + 1)
		weeklyRef := fmt.Sprintf("%s%d/%d/%d", maintenance.WeeklyReferencePrefix, account.userID, year, week)
		weekly := usageReportPayload(t, notifications, account.userID, weeklyRef)
		if weekly.TotalRequests != 2*factor || weekly.PrevRequests != factor || weekly.TotalVerifies != factor || weekly.PrevVerifies != factor {
			t.Errorf("account %d weekly totals = requests %d/%d verifies %d/%d", i, weekly.TotalRequests, weekly.PrevRequests, weekly.TotalVerifies, weekly.PrevVerifies)
		}

		monthlyRef := fmt.Sprintf("%s%d/%d/%d", maintenance.MonthlyReferencePrefix, account.userID, tnow.Year(), int(tnow.Month()))
		monthly := usageReportPayload(t, notifications, account.userID, monthlyRef)
		if monthly.TotalRequests != 3*factor || monthly.PrevRequests != 3*factor || monthly.TotalVerifies != 2*factor || monthly.PrevVerifies != factor {
			t.Errorf("account %d monthly totals = requests %d/%d verifies %d/%d", i, monthly.TotalRequests, monthly.PrevRequests, monthly.TotalVerifies, monthly.PrevVerifies)
		}
	}
}

func TestRetrieveAccountReportStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"-active", testPlan)
	if err != nil {
		t.Fatalf("failed to create active account: %v", err)
	}
	zeroUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"-zero", testPlan)
	if err != nil {
		t.Fatalf("failed to create zero-activity account: %v", err)
	}
	property, err := db_tests.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatalf("failed to create property: %v", err)
	}

	to := truncateToMonth(recentMonthlyReportTime(time.Now().UTC()))
	from := to.AddDate(0, -2, 0)
	mid := to.AddDate(0, -1, 0)
	if err := timeSeries.WriteAccessLogBatch(ctx, []*common.AccessRecord{
		{UserID: user.ID, OrgID: org.ID, PropertyID: property.ID, Timestamp: from.AddDate(0, 0, 1)},
		{UserID: user.ID, OrgID: org.ID, PropertyID: property.ID, Timestamp: mid.AddDate(0, 0, 1)},
		{UserID: user.ID, OrgID: org.ID, PropertyID: property.ID, Timestamp: mid.AddDate(0, 0, 2)},
	}); err != nil {
		t.Fatalf("failed to seed access logs: %v", err)
	}
	if err := timeSeries.WriteVerifyLogBatch(ctx, []*common.VerifyRecord{
		{UserID: user.ID, OrgID: org.ID, PropertyID: property.ID, Timestamp: from.AddDate(0, 0, 1), Status: 0},
		{UserID: user.ID, OrgID: org.ID, PropertyID: property.ID, Timestamp: mid.AddDate(0, 0, 1), Status: 1},
	}); err != nil {
		t.Fatalf("failed to seed verification logs: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	userIDs := []int32{user.ID, zeroUser.ID}
	dailyStats, err := timeSeries.RetrieveWeeklyAccountReportStats(ctx, userIDs, from, mid, to)
	if err != nil {
		t.Fatalf("failed to retrieve daily account report stats: %v", err)
	}
	monthlyStats, err := timeSeries.RetrieveMonthlyAccountReportStats(ctx, userIDs, from, mid, to)
	if err != nil {
		t.Fatalf("failed to retrieve monthly account report stats: %v", err)
	}
	partialStats, err := timeSeries.RetrieveMonthlyAccountReportStats(ctx, userIDs, from.AddDate(0, 0, 1), mid.AddDate(0, 0, 1), to.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("failed to retrieve partial-month account report stats: %v", err)
	}

	for name, stats := range map[string]map[int32]*common.UserReportAccountStats{
		"daily":         dailyStats,
		"monthly":       monthlyStats,
		"partial month": partialStats,
	} {
		active := stats[user.ID]
		if active == nil {
			t.Fatalf("%s stats omitted active user", name)
		}
		if active.CurrentRequests != 2 || active.PrevRequests != 1 || active.CurrentVerifies != 1 || active.PrevVerifies != 1 {
			t.Errorf("%s active-user stats = %+v, want requests 2/1 and verifications 1/1", name, active)
		}
		zero := stats[zeroUser.ID]
		if zero == nil {
			t.Fatalf("%s stats omitted zero-activity user", name)
		}
		if *zero != (common.UserReportAccountStats{}) {
			t.Errorf("%s zero-activity stats = %+v, want zero values", name, zero)
		}
	}
}

func TestRetrievePropertyReportCandidates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create account: %v", err)
	}
	requestHeavyProperty := createReportPropertyForTest(t, ctx, user.ID, org, "request-heavy.reports-test.org")
	lowerVolumeFailureProperty := createReportPropertyForTest(t, ctx, user.ID, org, "lower-volume-failure.reports-test.org")
	highVolumeFailureProperty := createReportPropertyForTest(t, ctx, user.ID, org, "high-volume-failure.reports-test.org")

	to := truncateDayForTest(recentWeeklyReportTime(time.Now().UTC()))
	mid := to.AddDate(0, 0, -7)
	from := to.AddDate(0, 0, -14)
	requestHeavyDay := mid.AddDate(0, 0, 1).Add(12 * time.Hour)
	failureHeavyDay := mid.AddDate(0, 0, 2).Add(12 * time.Hour)
	boundaryDay := mid.AddDate(0, 0, 3).Add(12 * time.Hour)
	nonQualifyingDay := mid.AddDate(0, 0, 4).Add(12 * time.Hour)
	higherRatioDay := mid.AddDate(0, 0, 5).Add(12 * time.Hour)
	secondHighVolumeFailureDay := mid.Add(12 * time.Hour)
	thirdHighVolumeFailureDay := mid.AddDate(0, 0, 6).Add(12 * time.Hour)

	accessLogs := make([]*common.AccessRecord, 0)
	verifyLogs := make([]*common.VerifyRecord, 0)
	addRequests := func(propertyID int32, at time.Time, count int) {
		for range count {
			accessLogs = append(accessLogs, &common.AccessRecord{UserID: user.ID, OrgID: org.ID, PropertyID: propertyID, Timestamp: at})
		}
	}
	addVerifies := func(propertyID int32, at time.Time, success, failure int) {
		for range success {
			verifyLogs = append(verifyLogs, &common.VerifyRecord{UserID: user.ID, OrgID: org.ID, PropertyID: propertyID, Timestamp: at})
		}
		for range failure {
			verifyLogs = append(verifyLogs, &common.VerifyRecord{UserID: user.ID, OrgID: org.ID, PropertyID: propertyID, Timestamp: at, Status: 1})
		}
	}

	addRequests(requestHeavyProperty.ID, requestHeavyDay, 8)
	addVerifies(requestHeavyProperty.ID, requestHeavyDay, 2, 0)
	addRequests(requestHeavyProperty.ID, nonQualifyingDay, 1)
	addVerifies(requestHeavyProperty.ID, nonQualifyingDay, 1, 0)
	addRequests(requestHeavyProperty.ID, mid.Add(-time.Hour), 2)
	addRequests(lowerVolumeFailureProperty.ID, failureHeavyDay, 1)
	addVerifies(lowerVolumeFailureProperty.ID, failureHeavyDay, 0, 5)
	addRequests(highVolumeFailureProperty.ID, boundaryDay, 2)
	addVerifies(highVolumeFailureProperty.ID, boundaryDay, 0, 6)
	addRequests(highVolumeFailureProperty.ID, higherRatioDay, 1)
	addVerifies(highVolumeFailureProperty.ID, higherRatioDay, 0, 8)
	addRequests(highVolumeFailureProperty.ID, secondHighVolumeFailureDay, 1)
	addVerifies(highVolumeFailureProperty.ID, secondHighVolumeFailureDay, 0, 7)
	addRequests(highVolumeFailureProperty.ID, thirdHighVolumeFailureDay, 1)
	addVerifies(highVolumeFailureProperty.ID, thirdHighVolumeFailureDay, 0, 6)
	if err := timeSeries.WriteAccessLogBatch(ctx, accessLogs); err != nil {
		t.Fatalf("failed to seed access logs: %v", err)
	}
	if err := timeSeries.WriteVerifyLogBatch(ctx, verifyLogs); err != nil {
		t.Fatalf("failed to seed verification logs: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	stats, err := timeSeries.RetrieveWeeklyPropertiesReportStats(ctx, user.ID, from, mid, to, common.UserReportOptions{
		TopPropertiesLimit:                2,
		SecurityEventsLimit:               4,
		SecurityEventsPerPropertyLimit:    2,
		SecurityEventRatioThreshold:       3,
		SecurityEventMinimumDominantCount: 4,
	})
	if err != nil {
		t.Fatalf("failed to retrieve property report stats: %v", err)
	}
	if len(stats.Properties) != 2 {
		t.Fatalf("properties count = %d, want 2", len(stats.Properties))
	}
	requestHeavyStat := reportPropertyStatForTest(t, stats.Properties, requestHeavyProperty.ID)
	if requestHeavyStat.CurrentRequests != 9 || requestHeavyStat.PrevRequests != 2 {
		t.Errorf("request-heavy property = %+v, want current/previous requests 9/2", requestHeavyStat)
	}
	highVolumeFailureStat := reportPropertyStatForTest(t, stats.Properties, highVolumeFailureProperty.ID)
	if highVolumeFailureStat.CurrentRequests != 5 || highVolumeFailureStat.PrevRequests != 0 {
		t.Errorf("high-volume failure property = %+v, want current/previous requests 5/0", highVolumeFailureStat)
	}
	if len(stats.SecurityEvents) != 4 {
		t.Fatalf("security events count = %d, want 4", len(stats.SecurityEvents))
	}
	highVolumeFailureCounts := make(map[uint64]bool)
	for _, candidate := range stats.SecurityEvents {
		if candidate.PropertyID == highVolumeFailureProperty.ID {
			highVolumeFailureCounts[candidate.FailedVerifies] = true
		}
	}
	if len(highVolumeFailureCounts) != 2 || !highVolumeFailureCounts[8] || !highVolumeFailureCounts[7] {
		t.Errorf("high-volume failure counts = %v, want only 8 and 7", highVolumeFailureCounts)
	}
	if candidate := reportSecurityEventByIDForTest(t, stats.SecurityEvents, requestHeavyProperty.ID); candidate.Requests != 8 || candidate.Verifies != 2 || candidate.FailedVerifies != 0 {
		t.Errorf("request-heavy candidate = %+v, want 8 requests and 2 verifications", candidate)
	}
	if candidate := reportSecurityEventByIDForTest(t, stats.SecurityEvents, lowerVolumeFailureProperty.ID); candidate.Requests != 1 || candidate.Verifies != 5 || candidate.FailedVerifies != 5 {
		t.Errorf("lower-volume candidate = %+v, want 1 request and 5 failed verifications", candidate)
	}
	limited, err := timeSeries.RetrieveWeeklyPropertiesReportStats(ctx, user.ID, from, mid, to, common.UserReportOptions{
		TopPropertiesLimit:                2,
		SecurityEventsLimit:               2,
		SecurityEventsPerPropertyLimit:    2,
		SecurityEventRatioThreshold:       3,
		SecurityEventMinimumDominantCount: 4,
	})
	if err != nil {
		t.Fatalf("failed to retrieve limited property report stats: %v", err)
	}
	if len(limited.SecurityEvents) != 2 {
		t.Fatalf("limited candidates count = %d, want 2", len(limited.SecurityEvents))
	}
	reportSecurityEventByIDForTest(t, limited.SecurityEvents, requestHeavyProperty.ID)
	reportSecurityEventByIDForTest(t, limited.SecurityEvents, highVolumeFailureProperty.ID)

	withoutCandidates, err := timeSeries.RetrieveWeeklyPropertiesReportStats(ctx, user.ID, from, mid, to, common.UserReportOptions{
		TopPropertiesLimit:                2,
		SecurityEventsLimit:               0,
		SecurityEventsPerPropertyLimit:    2,
		SecurityEventRatioThreshold:       3,
		SecurityEventMinimumDominantCount: 4,
	})
	if err != nil {
		t.Fatalf("failed to retrieve property report stats without candidates: %v", err)
	}
	if len(withoutCandidates.Properties) != 2 || len(withoutCandidates.SecurityEvents) != 0 {
		t.Errorf("zero-limit result has %d properties and %d candidates, want 2 and 0", len(withoutCandidates.Properties), len(withoutCandidates.SecurityEvents))
	}
	reportPropertyStatForTest(t, withoutCandidates.Properties, requestHeavyProperty.ID)
	reportPropertyStatForTest(t, withoutCandidates.Properties, highVolumeFailureProperty.ID)
}

func TestRetrievePropertyReportCandidatesWithZeroDenominators(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create account: %v", err)
	}
	requestHeavyProperty := createReportPropertyForTest(t, ctx, user.ID, org, "zero-verifications.reports-test.org")
	failureHeavyProperty := createReportPropertyForTest(t, ctx, user.ID, org, "zero-requests.reports-test.org")

	to := truncateDayForTest(recentWeeklyReportTime(time.Now().UTC()))
	mid := to.AddDate(0, 0, -7)
	from := to.AddDate(0, 0, -14)
	day := mid.AddDate(0, 0, 1)
	accessLogs := make([]*common.AccessRecord, 100)
	verifyLogs := make([]*common.VerifyRecord, 100)
	for i := range 100 {
		accessLogs[i] = &common.AccessRecord{UserID: user.ID, OrgID: org.ID, PropertyID: requestHeavyProperty.ID, Timestamp: day}
		verifyLogs[i] = &common.VerifyRecord{UserID: user.ID, OrgID: org.ID, PropertyID: failureHeavyProperty.ID, Timestamp: day, Status: 1}
	}
	if err := timeSeries.WriteAccessLogBatch(ctx, accessLogs); err != nil {
		t.Fatalf("failed to seed access logs: %v", err)
	}
	if err := timeSeries.WriteVerifyLogBatch(ctx, verifyLogs); err != nil {
		t.Fatalf("failed to seed verification logs: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	stats, err := timeSeries.RetrieveWeeklyPropertiesReportStats(ctx, user.ID, from, mid, to, common.UserReportOptions{
		TopPropertiesLimit:                2,
		SecurityEventsLimit:               2,
		SecurityEventsPerPropertyLimit:    2,
		SecurityEventRatioThreshold:       3,
		SecurityEventMinimumDominantCount: 100,
	})
	if err != nil {
		t.Fatalf("failed to retrieve property report stats: %v", err)
	}
	if len(stats.SecurityEvents) != 2 {
		t.Fatalf("security events count = %d, want 2", len(stats.SecurityEvents))
	}
	requestHeavy := reportSecurityEventByIDForTest(t, stats.SecurityEvents, requestHeavyProperty.ID)
	if requestHeavy.Requests != 100 || requestHeavy.Verifies != 0 {
		t.Errorf("request-heavy candidate = %+v, want 100 requests and zero verifications", requestHeavy)
	}
	failureHeavy := reportSecurityEventByIDForTest(t, stats.SecurityEvents, failureHeavyProperty.ID)
	if failureHeavy.Requests != 0 || failureHeavy.Verifies != 100 || failureHeavy.FailedVerifies != 100 {
		t.Errorf("failure-heavy candidate = %+v, want 100 failed verifications and zero requests", failureHeavy)
	}
}

func reportPropertyStatForTest(t *testing.T, stats []*common.UserReportPropertyStat, propertyID int32) *common.UserReportPropertyStat {
	t.Helper()
	for _, stat := range stats {
		if stat.PropertyID == propertyID {
			return stat
		}
	}
	t.Fatalf("report property stat for property %d not found in %+v", propertyID, stats)
	return nil
}

func createReportPropertyForTest(t *testing.T, ctx context.Context, userID int32, org *dbgen.Organization, name string) *dbgen.Property {
	t.Helper()
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(userID, name), org)
	if err != nil {
		t.Fatalf("failed to create report property %q: %v", name, err)
	}
	return property
}

func reportSecurityEventByIDForTest(t *testing.T, candidates []*common.UserReportSecurityEvent, propertyID int32) *common.UserReportSecurityEvent {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.PropertyID == propertyID {
			return candidate
		}
	}
	t.Fatalf("security event for property %d not found in %+v", propertyID, candidates)
	return nil
}

func truncateToMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func truncateDayForTest(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func TestWeeklyReportDedup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	_, _, err = store.Impl().UpsertUserSettings(ctx, &dbgen.UpsertUserSettingsParams{
		UserID:       user.ID,
		WeeklyReport: true,
	})
	if err != nil {
		t.Fatalf("failed to upsert user settings: %v", err)
	}

	job := newScheduleReportsJob(50)

	// Monday so weekly reports trigger
	tnow := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	year, week := tnow.ISOWeek()
	refSuffix := fmt.Sprintf("/%d/%d", year, week)

	// before running the job, user should be in the pending list
	pendingBefore, err := store.Impl().RetrieveUsersWithPendingWeeklyReport(ctx, 100, 0, maintenance.WeeklyReferencePrefix, refSuffix, job.PlanService.ExpiredTrialStatus())
	if err != nil {
		t.Fatalf("RetrieveUsersWithPendingWeeklyReport failed: %v", err)
	}
	var foundBefore bool
	for _, u := range pendingBefore {
		if u.UserID == user.ID {
			foundBefore = true
			break
		}
	}
	if !foundBefore {
		t.Fatalf("expected user %d in pending weekly report list before job run", user.ID)
	}

	params := &maintenance.ScheduleReportsParams{
		UsersLimit: 50,
		UserID:     user.ID,
		Weekly:     true,
		Monthly:    false,
	}

	if err := job.RunOnceAt(ctx, params, tnow); err != nil {
		t.Fatalf("RunOnceAt failed: %v", err)
	}

	// after running the job, user should be absent from the pending list
	pendingAfter, err := store.Impl().RetrieveUsersWithPendingWeeklyReport(ctx, 100, 0, maintenance.WeeklyReferencePrefix, refSuffix, job.PlanService.ExpiredTrialStatus())
	if err != nil {
		t.Fatalf("RetrieveUsersWithPendingWeeklyReport failed: %v", err)
	}
	for _, u := range pendingAfter {
		if u.UserID == user.ID {
			t.Errorf("user %d should not be in pending weekly report list after job run", user.ID)
		}
	}
}

func TestMonthlyReportDedup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	_, _, err = store.Impl().UpsertUserSettings(ctx, &dbgen.UpsertUserSettingsParams{
		UserID:        user.ID,
		MonthlyReport: true,
	})
	if err != nil {
		t.Fatalf("failed to upsert user settings: %v", err)
	}

	job := newScheduleReportsJob(50)

	// 1st of month so monthly reports trigger
	tnow := time.Date(2025, 4, 1, 10, 0, 0, 0, time.UTC)
	refSuffix := fmt.Sprintf("/%d/%d", tnow.Year(), int(tnow.Month()))

	// before running the job, user should be in the pending list
	pendingBefore, err := store.Impl().RetrieveUsersWithPendingMonthlyReport(ctx, 100, 0, maintenance.MonthlyReferencePrefix, refSuffix, job.PlanService.ExpiredTrialStatus())
	if err != nil {
		t.Fatalf("RetrieveUsersWithPendingMonthlyReport failed: %v", err)
	}
	var foundBefore bool
	for _, u := range pendingBefore {
		if u.UserID == user.ID {
			foundBefore = true
			break
		}
	}
	if !foundBefore {
		t.Fatalf("expected user %d in pending monthly report list before job run", user.ID)
	}

	params := &maintenance.ScheduleReportsParams{
		UsersLimit: 50,
		UserID:     user.ID,
		Weekly:     false,
		Monthly:    true,
	}

	if err := job.RunOnceAt(ctx, params, tnow); err != nil {
		t.Fatalf("RunOnceAt failed: %v", err)
	}

	// after running the job, user should be absent from the pending list
	pendingAfter, err := store.Impl().RetrieveUsersWithPendingMonthlyReport(ctx, 100, 0, maintenance.MonthlyReferencePrefix, refSuffix, job.PlanService.ExpiredTrialStatus())
	if err != nil {
		t.Fatalf("RetrieveUsersWithPendingMonthlyReport failed: %v", err)
	}
	for _, u := range pendingAfter {
		if u.UserID == user.ID {
			t.Errorf("user %d should not be in pending monthly report list after job run", user.ID)
		}
	}
}

func seedTimeSeries(t *testing.T, ts *db.MemoryTimeSeries, userID int32, propID, orgID int32, timestamp time.Time, count int) {
	t.Helper()
	ctx := context.Background()
	records := make([]*common.AccessRecord, count)
	for i := range records {
		records[i] = &common.AccessRecord{
			UserID:     userID,
			PropertyID: propID,
			OrgID:      orgID,
			Timestamp:  timestamp.Add(time.Duration(i) * time.Minute),
		}
	}
	if err := ts.WriteAccessLogBatch(ctx, records); err != nil {
		t.Fatal(err)
	}
}

func seedVerifyLogs(t *testing.T, ts *db.MemoryTimeSeries, userID int32, propID, orgID int32, timestamp time.Time, count int) {
	t.Helper()
	ctx := context.Background()
	records := make([]*common.VerifyRecord, count)
	for i := range records {
		records[i] = &common.VerifyRecord{
			UserID:     userID,
			PropertyID: propID,
			OrgID:      orgID,
			Timestamp:  timestamp.Add(time.Duration(i) * time.Minute),
			Status:     1,
		}
	}
	if err := ts.WriteVerifyLogBatch(ctx, records); err != nil {
		t.Fatal(err)
	}
}

func seedFormSubmitLogs(t *testing.T, ts *db.MemoryTimeSeries, userID int32, formID, orgID int32, timestamp time.Time, status int8, count int) {
	t.Helper()
	ctx := context.Background()
	records := make([]*common.FormSubmitRecord, count)
	for i := range records {
		records[i] = &common.FormSubmitRecord{
			UserID:    userID,
			FormID:    formID,
			OrgID:     orgID,
			Timestamp: timestamp.Add(time.Duration(i) * time.Minute),
			Status:    status,
		}
	}
	if err := ts.WriteFormSubmitBatch(ctx, records); err != nil {
		t.Fatal(err)
	}
}

func seedFormSubmitLogsToStore(t *testing.T, ts common.TimeSeriesStore, userID int32, formID, orgID int32, timestamp time.Time, status int8, count int) {
	t.Helper()
	ctx := context.Background()
	records := make([]*common.FormSubmitRecord, count)
	for i := range records {
		records[i] = &common.FormSubmitRecord{
			UserID:    userID,
			FormID:    formID,
			OrgID:     orgID,
			Timestamp: timestamp.Add(time.Duration(i) * time.Minute),
			Status:    status,
		}
	}
	if err := ts.WriteFormSubmitBatch(ctx, records); err != nil {
		t.Fatal(err)
	}
}

func recentWeeklyReportTime(now time.Time) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.UTC)
	daysSinceMonday := (int(t.Weekday()) - int(time.Monday) + 7) % 7
	t = t.AddDate(0, 0, -daysSinceMonday)
	if t.After(now) {
		t = t.AddDate(0, 0, -7)
	}
	return t
}

func recentMonthlyReportTime(now time.Time) time.Time {
	t := time.Date(now.Year(), now.Month(), 1, 10, 0, 0, 0, time.UTC)
	if t.After(now) {
		t = t.AddDate(0, -1, 0)
	}
	return t
}

func usageReportPayload(t *testing.T, notifications []*dbgen.GetPendingUserNotificationsRow, userID int32, referenceID string) *email.UsageReportContext {
	t.Helper()
	var payload *email.UsageReportContext
	matches := 0
	for _, n := range notifications {
		if n.UserNotification.UserID.Int32 != userID || n.UserNotification.ReferenceID != referenceID {
			continue
		}

		matches++
		if matches > 1 {
			t.Fatalf("expected exactly 1 notification for user %d with reference %q, got %d", userID, referenceID, matches)
		}
		payload = &email.UsageReportContext{}
		if err := json.Unmarshal(n.UserNotification.Payload, payload); err != nil {
			t.Fatalf("failed to unmarshal notification payload: %v", err)
		}
	}

	if matches == 0 {
		t.Fatalf("notification not found for user %d with reference %q", userID, referenceID)
	}
	return payload
}

func TestBuildWeeklyReport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	prop1, err := db_tests.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatalf("failed to create property 1: %v", err)
	}
	prop2Params := db_tests.CreateNewPropertyParams(user.ID, "blog.reports-test.org")
	prop2, _, err := store.Impl().CreateNewProperty(ctx, prop2Params, org)
	if err != nil {
		t.Fatalf("failed to create property 2: %v", err)
	}
	form1, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "contact.reports-test.org"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("failed to create form 1: %v", err)
	}
	form2, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "support.reports-test.org"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("failed to create form 2: %v", err)
	}

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	t.Run("WithData", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 50)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 80)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, from, 40)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, mid, 0, 20)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, mid, 1, 5)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, from, 0, 10)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, from, 1, 2)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.Period != "weekly" {
			t.Errorf("expected period 'weekly', got %q", result.Period)
		}
		if result.TotalRequests != 100 {
			t.Errorf("expected TotalRequests=100, got %d", result.TotalRequests)
		}
		if result.PrevRequests != 80 {
			t.Errorf("expected PrevRequests=80, got %d", result.PrevRequests)
		}
		if result.TotalVerifies != 50 {
			t.Errorf("expected TotalVerifies=50, got %d", result.TotalVerifies)
		}
		if result.PrevVerifies != 40 {
			t.Errorf("expected PrevVerifies=40, got %d", result.PrevVerifies)
		}
		if result.RequestsChange <= 0 {
			t.Errorf("expected positive RequestsChange, got %f", result.RequestsChange)
		}
		if result.VerifiesChange <= 0 {
			t.Errorf("expected positive VerifiesChange, got %f", result.VerifiesChange)
		}
		if result.VerificationRate == 0 {
			t.Error("expected non-zero VerificationRate")
		}
		if result.TotalFormSubmissions != 25 {
			t.Errorf("expected TotalFormSubmissions=25, got %d", result.TotalFormSubmissions)
		}
		if result.PrevFormSubmissions != 12 {
			t.Errorf("expected PrevFormSubmissions=12, got %d", result.PrevFormSubmissions)
		}
		if result.TotalFormErrors != 5 {
			t.Errorf("expected TotalFormErrors=5, got %d", result.TotalFormErrors)
		}
		if result.PrevFormErrors != 2 {
			t.Errorf("expected PrevFormErrors=2, got %d", result.PrevFormErrors)
		}
		if result.FormSubmissionsChange <= 0 {
			t.Errorf("expected positive FormSubmissionsChange, got %f", result.FormSubmissionsChange)
		}
		if result.FormErrorsChange <= 0 {
			t.Errorf("expected positive FormErrorsChange, got %f", result.FormErrorsChange)
		}
		if result.FormErrorRate <= 0 {
			t.Errorf("expected positive FormErrorRate, got %f", result.FormErrorRate)
		}
	})

	t.Run("NoData", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()
		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.TotalRequests != 0 {
			t.Errorf("expected TotalRequests=0, got %d", result.TotalRequests)
		}
		if result.TotalVerifies != 0 {
			t.Errorf("expected TotalVerifies=0, got %d", result.TotalVerifies)
		}
		if result.PrevRequests != 0 {
			t.Errorf("expected PrevRequests=0, got %d", result.PrevRequests)
		}
		if result.VerificationRate != 0 {
			t.Errorf("expected VerificationRate=0, got %f", result.VerificationRate)
		}
		if len(result.TopProperties) != 0 {
			t.Errorf("expected no TopProperties, got %d", len(result.TopProperties))
		}
		if result.RequestsChange != 0 {
			t.Errorf("expected RequestsChange=0, got %f", result.RequestsChange)
		}
		if result.TotalFormSubmissions != 0 {
			t.Errorf("expected TotalFormSubmissions=0, got %d", result.TotalFormSubmissions)
		}
		if result.TotalFormErrors != 0 {
			t.Errorf("expected TotalFormErrors=0, got %d", result.TotalFormErrors)
		}
		if result.PrevFormSubmissions != 0 {
			t.Errorf("expected PrevFormSubmissions=0, got %d", result.PrevFormSubmissions)
		}
		if result.FormErrorRate != 0 {
			t.Errorf("expected FormErrorRate=0, got %f", result.FormErrorRate)
		}
	})

	t.Run("NoPreviousPeriod", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 50)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 30)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.TotalRequests != 50 {
			t.Errorf("expected TotalRequests=50, got %d", result.TotalRequests)
		}
		if result.PrevRequests != 0 {
			t.Errorf("expected PrevRequests=0, got %d", result.PrevRequests)
		}
		if result.RequestsChange != 100 {
			t.Errorf("expected RequestsChange=100, got %f", result.RequestsChange)
		}
	})

	t.Run("DecreaseShowsNegativeChange", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 30)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 20)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 60)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, from, 40)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.RequestsChange >= 0 {
			t.Errorf("expected negative RequestsChange, got %f", result.RequestsChange)
		}
		if result.VerifiesChange >= 0 {
			t.Errorf("expected negative VerifiesChange, got %f", result.VerifiesChange)
		}
	})

	t.Run("NoChangeShowsZero", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 50)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 50)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.RequestsChange != 0 {
			t.Errorf("expected RequestsChange=0, got %f", result.RequestsChange)
		}
	})

	t.Run("TopProperties", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedTimeSeries(t, ts, user.ID, prop2.ID, org.ID, mid, 50)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if len(result.TopProperties) != 2 {
			t.Fatalf("expected 2 TopProperties, got %d", len(result.TopProperties))
		}

		if result.TopProperties[0].Count != 100 {
			t.Errorf("expected first property count=100, got %d", result.TopProperties[0].Count)
		}
		if result.TopProperties[0].Alternate {
			t.Error("expected first property row to be unstriped")
		}
		if !result.TopProperties[1].Alternate {
			t.Error("expected second property row to be striped")
		}
	})

	t.Run("SecurityEvents", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()
		day := mid.AddDate(0, 0, 1)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, day, 400)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, day, 100)
		failureDay := mid.AddDate(0, 0, 2)
		seedTimeSeries(t, ts, user.ID, prop2.ID, org.ID, failureDay, 25)
		seedVerifyLogs(t, ts, user.ID, prop2.ID, org.ID, failureDay, 100)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if len(result.SecurityEvents) != 2 {
			t.Fatalf("security events count = %d, want 2", len(result.SecurityEvents))
		}
		highlight := reportSecurityEventByNameForTest(t, result.SecurityEvents, prop1.Name)
		if highlight.Name != prop1.Name || highlight.Date != day.Format(time.DateOnly) || highlight.Requests != 400 || highlight.Verifies != 100 {
			t.Errorf("security event = %+v, want request-heavy property", highlight)
		}
		if highlight.FailedVerifies != 0 {
			t.Errorf("request-heavy highlight failed verifications = %d, want 0", highlight.FailedVerifies)
		}
		failureHighlight := reportSecurityEventByNameForTest(t, result.SecurityEvents, prop2.Name)
		if failureHighlight.FailedVerifies != 100 {
			t.Errorf("failure-heavy highlight failed verifications = %d, want 100", failureHighlight.FailedVerifies)
		}
		expectedLink := reportPortalURL + "/org/" + server.IDHasher.Encrypt(int(org.ID)) + "/property/" + server.IDHasher.Encrypt(int(prop1.ID)) + "?" + result.UTM
		if highlight.Link != expectedLink {
			t.Errorf("security event link = %q, want %q", highlight.Link, expectedLink)
		}
	})

	t.Run("ProtectionHighlightLimits", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()
		seedProtectionEvent := func(propertyID int32, day, requests, verifies int) {
			at := mid.AddDate(0, 0, day)
			seedTimeSeries(t, ts, user.ID, propertyID, org.ID, at, requests)
			seedVerifyLogs(t, ts, user.ID, propertyID, org.ID, at, verifies)
		}
		seedProtectionEvent(prop1.ID, 1, 500, 100)
		seedProtectionEvent(prop1.ID, 2, 400, 80)
		seedProtectionEvent(prop1.ID, 3, 300, 60)
		seedProtectionEvent(prop2.ID, 4, 250, 50)
		seedProtectionEvent(prop2.ID, 5, 200, 40)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.SecurityEvents) != 3 {
			t.Fatalf("weekly security events count = %d, want 3", len(result.SecurityEvents))
		}
		countsByProperty := make(map[string]int)
		for _, highlight := range result.SecurityEvents {
			countsByProperty[highlight.Name]++
		}
		if countsByProperty[prop1.Name] != 2 || countsByProperty[prop2.Name] != 1 {
			t.Errorf("weekly highlight counts by property = %v, want %q:2 and %q:1", countsByProperty, prop1.Name, prop2.Name)
		}
	})

	t.Run("PropertyChangeDirection", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 50)
		seedTimeSeries(t, ts, user.ID, prop2.ID, org.ID, mid, 30)
		seedTimeSeries(t, ts, user.ID, prop2.ID, org.ID, from, 60)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if len(result.TopProperties) != 2 {
			t.Fatalf("expected 2 TopProperties, got %d", len(result.TopProperties))
		}

		// prop1 has 100 current (up from 50)
		if result.TopProperties[0].Change <= 0 {
			t.Errorf("expected positive Change for increasing property, got %f", result.TopProperties[0].Change)
		}
		// prop2 has 30 current (down from 60)
		if result.TopProperties[1].Change >= 0 {
			t.Errorf("expected negative Change for decreasing property, got %f", result.TopProperties[1].Change)
		}
	})

	t.Run("VerificationRate", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 50)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		expectedRate := 50.0
		if result.VerificationRate != expectedRate {
			t.Errorf("expected VerificationRate=%f, got %f", expectedRate, result.VerificationRate)
		}
		if result.VerificationRateChange != 100 {
			t.Errorf("expected VerificationRateChange=100, got %f", result.VerificationRateChange)
		}
	})

	t.Run("VerificationRateChange", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 40)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 100)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, from, 50)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.VerificationRate != 40 {
			t.Errorf("expected VerificationRate=40, got %f", result.VerificationRate)
		}
		if result.VerificationRateChange >= 0 {
			t.Errorf("expected negative VerificationRateChange, got %f", result.VerificationRateChange)
		}
	})

	t.Run("TopForms", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, mid, 0, 100)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, from, 0, 50)
		seedFormSubmitLogs(t, ts, user.ID, form2.ID, org.ID, mid, 0, 30)
		seedFormSubmitLogs(t, ts, user.ID, form2.ID, org.ID, from, 0, 60)
		seedFormSubmitLogs(t, ts, user.ID, 999999, org.ID, mid, 0, 200)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if len(result.TopForms) != 2 {
			t.Fatalf("expected 2 TopForms, got %d", len(result.TopForms))
		}

		if result.TopForms[0].Count != 100 {
			t.Errorf("expected first form count=100, got %d", result.TopForms[0].Count)
		}
		if result.TopForms[0].Name != form1.Name {
			t.Errorf("expected first form name=%q, got %q", form1.Name, result.TopForms[0].Name)
		}
		expectedLink := reportPortalURL + "/org/" + server.IDHasher.Encrypt(int(org.ID)) + "/form/" + server.IDHasher.Encrypt(int(form1.ID))
		if !strings.Contains(result.TopForms[0].Link, expectedLink) {
			t.Errorf("expected first form link=%q, got %q", expectedLink, result.TopForms[0].Link)
		}
		if result.TopForms[0].Alternate {
			t.Error("expected first form row to be unstriped")
		}
		if !result.TopForms[1].Alternate {
			t.Error("expected second form row to be striped")
		}
	})

	t.Run("FormChangeDirection", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, mid, 0, 100)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, from, 0, 50)
		seedFormSubmitLogs(t, ts, user.ID, form2.ID, org.ID, mid, 0, 30)
		seedFormSubmitLogs(t, ts, user.ID, form2.ID, org.ID, from, 0, 60)

		result, err := newWeeklyReport(ts).BuildWeeklyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if len(result.TopForms) != 2 {
			t.Fatalf("expected 2 TopForms, got %d", len(result.TopForms))
		}

		if result.TopForms[0].Change <= 0 {
			t.Errorf("expected positive Change for increasing form, got %f", result.TopForms[0].Change)
		}
		if result.TopForms[1].Change >= 0 {
			t.Errorf("expected negative Change for decreasing form, got %f", result.TopForms[1].Change)
		}
	})
}

func reportSecurityEventByNameForTest(t *testing.T, highlights []*email.SecurityEventStat, propertyName string) *email.SecurityEventStat {
	t.Helper()
	for _, highlight := range highlights {
		if highlight.Name == propertyName {
			return highlight
		}
	}
	t.Fatalf("security event for property %q not found in %+v", propertyName, highlights)
	return nil
}

func TestBuildMonthlyReport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	prop1, err := db_tests.CreatePropertyForOrg(ctx, store, org)
	if err != nil {
		t.Fatalf("failed to create property: %v", err)
	}
	form1, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "monthly-contact.reports-test.org"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("failed to create monthly form 1: %v", err)
	}
	form2, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "monthly-support.reports-test.org"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("failed to create monthly form 2: %v", err)
	}

	now := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, -1, 0)
	from := now.AddDate(0, -2, 0)

	t.Run("WithData", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 200)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 150)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, from, 80)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, mid, 0, 40)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, mid, 1, 10)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, from, 0, 20)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, from, 1, 5)

		result, err := newMonthlyReport(ts).BuildMonthlyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.Period != "monthly" {
			t.Errorf("expected period 'monthly', got %q", result.Period)
		}
		if result.TotalRequests != 200 {
			t.Errorf("expected TotalRequests=200, got %d", result.TotalRequests)
		}
		if result.PrevRequests != 150 {
			t.Errorf("expected PrevRequests=150, got %d", result.PrevRequests)
		}
		if result.TotalFormSubmissions != 50 {
			t.Errorf("expected TotalFormSubmissions=50, got %d", result.TotalFormSubmissions)
		}
		if result.PrevFormSubmissions != 25 {
			t.Errorf("expected PrevFormSubmissions=25, got %d", result.PrevFormSubmissions)
		}
		if result.TotalFormErrors != 10 {
			t.Errorf("expected TotalFormErrors=10, got %d", result.TotalFormErrors)
		}
		if result.PrevFormErrors != 5 {
			t.Errorf("expected PrevFormErrors=5, got %d", result.PrevFormErrors)
		}
	})

	t.Run("NoData", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()
		result, err := newMonthlyReport(ts).BuildMonthlyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.TotalRequests != 0 {
			t.Errorf("expected TotalRequests=0, got %d", result.TotalRequests)
		}
		if result.Period != "monthly" {
			t.Errorf("expected period 'monthly', got %q", result.Period)
		}
		if result.TotalFormSubmissions != 0 {
			t.Errorf("expected TotalFormSubmissions=0, got %d", result.TotalFormSubmissions)
		}
		if len(result.TopForms) != 0 {
			t.Errorf("expected no TopForms, got %d", len(result.TopForms))
		}
	})

	t.Run("NoPreviousPeriod", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 70)

		result, err := newMonthlyReport(ts).BuildMonthlyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.TotalRequests != 70 {
			t.Errorf("expected TotalRequests=70, got %d", result.TotalRequests)
		}
		if result.PrevRequests != 0 {
			t.Errorf("expected PrevRequests=0, got %d", result.PrevRequests)
		}
		if result.RequestsChange != 100 {
			t.Errorf("expected RequestsChange=100, got %f", result.RequestsChange)
		}
	})

	t.Run("ProtectionHighlightLimits", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()
		seedProtectionEvent := func(propertyID int32, day, requests, verifies int) {
			at := mid.AddDate(0, 0, day)
			seedTimeSeries(t, ts, user.ID, propertyID, org.ID, at, requests)
			seedVerifyLogs(t, ts, user.ID, propertyID, org.ID, at, verifies)
		}
		seedProtectionEvent(prop1.ID, 1, 600, 100)
		seedProtectionEvent(prop1.ID, 2, 500, 100)
		seedProtectionEvent(prop1.ID, 3, 400, 100)
		seedProtectionEvent(form1.PropertyID, 4, 300, 50)
		seedProtectionEvent(form1.PropertyID, 5, 200, 40)
		seedProtectionEvent(form2.PropertyID, 6, 150, 30)
		seedProtectionEvent(form2.PropertyID, 7, 100, 20)

		result, err := newMonthlyReport(ts).BuildMonthlyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.SecurityEvents) != 5 {
			t.Fatalf("monthly security events count = %d, want 5", len(result.SecurityEvents))
		}
		countsByProperty := make(map[string]int)
		for _, highlight := range result.SecurityEvents {
			countsByProperty[highlight.Name]++
			if countsByProperty[highlight.Name] > 2 {
				t.Errorf("monthly report has more than two highlights for property %q", highlight.Name)
			}
		}
		if len(countsByProperty) != 3 {
			t.Errorf("monthly report highlights %d properties, want 3: %v", len(countsByProperty), countsByProperty)
		}
	})

	t.Run("TopForms", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, mid, 0, 80)
		seedFormSubmitLogs(t, ts, user.ID, form1.ID, org.ID, from, 0, 40)
		seedFormSubmitLogs(t, ts, user.ID, form2.ID, org.ID, mid, 0, 30)
		seedFormSubmitLogs(t, ts, user.ID, form2.ID, org.ID, from, 0, 50)

		result, err := newMonthlyReport(ts).BuildMonthlyReport(ctx, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if len(result.TopForms) != 2 {
			t.Fatalf("expected 2 TopForms, got %d", len(result.TopForms))
		}
		if result.TopForms[0].Count != 80 {
			t.Errorf("expected first form count=80, got %d", result.TopForms[0].Count)
		}
		if result.TopForms[0].Change <= 0 {
			t.Errorf("expected positive Change for increasing form, got %f", result.TopForms[0].Change)
		}
		if result.TopForms[1].Change >= 0 {
			t.Errorf("expected negative Change for decreasing form, got %f", result.TopForms[1].Change)
		}
	})
}
