package portal

import (
	"fmt"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/jackc/pgx/v5/pgtype"
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

	notifications, err := store.Impl().RetrievePendingUserNotifications(ctx, tnow.Add(1*time.Minute), 100, 5)
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
		UserID:             user.ID,
		MonthlyReport:      true,
		NotificationsEmail: pgtype.Text{},
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

	notifications, err := store.Impl().RetrievePendingUserNotifications(ctx, tnow.Add(1*time.Minute), 100, 5)
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
