package portal

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func TestScheduleWeeklyReport(t *testing.T) {
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

	job := &maintenance.ScheduleReportsJob{
		Store:       store,
		TimeSeries:  timeSeries,
		PlanService: server.PlanService,
		UsersLimit:  50,
	}

	// Monday so weekly reports trigger
	tnow := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)

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

	var found bool
	for _, n := range notifications {
		if n.UserNotification.UserID.Int32 == user.ID && n.UserNotification.ReferenceID == expectedRef {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("weekly report notification not found for user %d with reference %q", user.ID, expectedRef)
	}
}

func TestScheduleMonthlyReport(t *testing.T) {
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

	job := &maintenance.ScheduleReportsJob{
		Store:       store,
		TimeSeries:  timeSeries,
		PlanService: server.PlanService,
		UsersLimit:  50,
	}

	// 1st of month so monthly reports trigger
	tnow := time.Date(2025, 4, 1, 10, 0, 0, 0, time.UTC)

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

	var found bool
	for _, n := range notifications {
		if n.UserNotification.UserID.Int32 == user.ID && n.UserNotification.ReferenceID == expectedRef {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("monthly report notification not found for user %d with reference %q", user.ID, expectedRef)
	}
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

	job := &maintenance.ScheduleReportsJob{
		Store:       store,
		TimeSeries:  timeSeries,
		PlanService: server.PlanService,
		UsersLimit:  50,
	}

	// Monday so weekly reports trigger
	tnow := time.Date(2025, 3, 17, 10, 0, 0, 0, time.UTC)
	year, week := tnow.ISOWeek()
	refSuffix := fmt.Sprintf("/%d/%d", year, week)

	// before running the job, user should be in the pending list
	pendingBefore, err := store.Impl().RetrieveUsersWithPendingWeeklyReport(ctx, 100, 0, maintenance.WeeklyReferencePrefix, refSuffix)
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
	pendingAfter, err := store.Impl().RetrieveUsersWithPendingWeeklyReport(ctx, 100, 0, maintenance.WeeklyReferencePrefix, refSuffix)
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

	job := &maintenance.ScheduleReportsJob{
		Store:       store,
		TimeSeries:  timeSeries,
		PlanService: server.PlanService,
		UsersLimit:  50,
	}

	// 1st of month so monthly reports trigger
	tnow := time.Date(2025, 4, 1, 10, 0, 0, 0, time.UTC)
	refSuffix := fmt.Sprintf("/%d/%d", tnow.Year(), int(tnow.Month()))

	// before running the job, user should be in the pending list
	pendingBefore, err := store.Impl().RetrieveUsersWithPendingMonthlyReport(ctx, 100, 0, maintenance.MonthlyReferencePrefix, refSuffix)
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
	pendingAfter, err := store.Impl().RetrieveUsersWithPendingMonthlyReport(ctx, 100, 0, maintenance.MonthlyReferencePrefix, refSuffix)
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

	now := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, 0, -7)
	from := now.AddDate(0, 0, -14)

	t.Run("WithData", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 50)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 80)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, from, 40)

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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
	})

	t.Run("NoData", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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
	})

	t.Run("NoPreviousPeriod", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 50)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 30)

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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

	t.Run("PropertyChangeDirection", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 50)
		seedTimeSeries(t, ts, user.ID, prop2.ID, org.ID, mid, 30)
		seedTimeSeries(t, ts, user.ID, prop2.ID, org.ID, from, 60)

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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

		result, err := maintenance.BuildWeeklyReport(ctx, store, ts, user.ID, from, mid, now)
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

	now := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	mid := now.AddDate(0, -1, 0)
	from := now.AddDate(0, -2, 0)

	t.Run("WithData", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 200)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, mid, 100)
		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, from, 150)
		seedVerifyLogs(t, ts, user.ID, prop1.ID, org.ID, from, 80)

		result, err := maintenance.BuildMonthlyReport(ctx, store, ts, user.ID, from, mid, now)
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
	})

	t.Run("NoData", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		result, err := maintenance.BuildMonthlyReport(ctx, store, ts, user.ID, from, mid, now)
		if err != nil {
			t.Fatal(err)
		}

		if result.TotalRequests != 0 {
			t.Errorf("expected TotalRequests=0, got %d", result.TotalRequests)
		}
		if result.Period != "monthly" {
			t.Errorf("expected period 'monthly', got %q", result.Period)
		}
	})

	t.Run("NoPreviousPeriod", func(t *testing.T) {
		ts := db.NewMemoryTimeSeries()

		seedTimeSeries(t, ts, user.ID, prop1.ID, org.ID, mid, 70)

		result, err := maintenance.BuildMonthlyReport(ctx, store, ts, user.ID, from, mid, now)
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
}
