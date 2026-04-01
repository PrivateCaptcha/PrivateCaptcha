package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

type ScheduleReportsJob struct {
	Store      db.Implementor
	TimeSeries common.TimeSeriesStore
}

var _ common.PeriodicJob = (*ScheduleReportsJob)(nil)

func (j *ScheduleReportsJob) Name() string {
	return "schedule_reports_job"
}

func (j *ScheduleReportsJob) Interval() time.Duration {
	return 1 * time.Hour
}

func (j *ScheduleReportsJob) Timeout() time.Duration {
	return 5 * time.Minute
}

func (j *ScheduleReportsJob) Jitter() time.Duration {
	return 10 * time.Minute
}

func (j *ScheduleReportsJob) Trigger() <-chan struct{} {
	return nil
}

func (j *ScheduleReportsJob) NewParams() any {
	return struct{}{}
}

func (j *ScheduleReportsJob) RunOnce(ctx context.Context, params any) error {
	tnow := time.Now().UTC()

	// Weekly reports: schedule on Mondays
	if tnow.Weekday() == time.Monday {
		if err := j.scheduleWeeklyReports(ctx, tnow); err != nil {
			slog.ErrorContext(ctx, "Failed to schedule weekly reports", common.ErrAttr(err))
		}
	}

	// Monthly reports: schedule on the 1st of each month
	if tnow.Day() == 1 {
		if err := j.scheduleMonthlyReports(ctx, tnow); err != nil {
			slog.ErrorContext(ctx, "Failed to schedule monthly reports", common.ErrAttr(err))
		}
	}

	return nil
}

func weeklyReportReference(userID int32, year int, week int) string {
	return fmt.Sprintf("report/weekly/%d/%d/%d", userID, year, week)
}

func monthlyReportReference(userID int32, year int, month time.Month) string {
	return fmt.Sprintf("report/monthly/%d/%d/%d", userID, year, month)
}

func (j *ScheduleReportsJob) scheduleWeeklyReports(ctx context.Context, tnow time.Time) error {
	users, err := j.Store.Impl().RetrieveUsersWithWeeklyReport(ctx)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Scheduling weekly reports", "users", len(users))
	year, week := tnow.ISOWeek()

	for _, user := range users {
		reportCtx := j.buildReportContext(ctx, user.UserID, "weekly")
		notif := &common.ScheduledNotification{
			ReferenceID:  weeklyReportReference(user.UserID, year, week),
			UserID:       user.UserID,
			Subject:      "[Private Captcha] Your weekly usage report",
			Data:         reportCtx,
			DateTime:     tnow,
			TemplateHash: email.WeeklyReportTemplate.Hash(),
			Persistent:   false,
			Condition:    common.EmptyNotificationCondition,
		}

		if _, err := j.Store.Impl().CreateUserNotification(ctx, notif); err != nil {
			slog.WarnContext(ctx, "Failed to create weekly report notification", "userID", user.UserID, common.ErrAttr(err))
		}
	}

	return nil
}

func (j *ScheduleReportsJob) scheduleMonthlyReports(ctx context.Context, tnow time.Time) error {
	users, err := j.Store.Impl().RetrieveUsersWithMonthlyReport(ctx)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Scheduling monthly reports", "users", len(users))

	for _, user := range users {
		reportCtx := j.buildReportContext(ctx, user.UserID, "monthly")
		notif := &common.ScheduledNotification{
			ReferenceID:  monthlyReportReference(user.UserID, tnow.Year(), tnow.Month()),
			UserID:       user.UserID,
			Subject:      "[Private Captcha] Your monthly usage report",
			Data:         reportCtx,
			DateTime:     tnow,
			TemplateHash: email.MonthlyReportTemplate.Hash(),
			Persistent:   false,
			Condition:    common.EmptyNotificationCondition,
		}

		if _, err := j.Store.Impl().CreateUserNotification(ctx, notif); err != nil {
			slog.WarnContext(ctx, "Failed to create monthly report notification", "userID", user.UserID, common.ErrAttr(err))
		}
	}

	return nil
}

func (j *ScheduleReportsJob) buildReportContext(ctx context.Context, userID int32, period string) *email.UsageReportContext {
	reportCtx := &email.UsageReportContext{
		Period:        period,
		DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint,
	}

	var from time.Time
	tnow := time.Now().UTC()
	if period == "weekly" {
		from = tnow.AddDate(0, 0, -7)
	} else {
		from = tnow.AddDate(0, -1, 0)
	}

	stats, err := j.TimeSeries.RetrieveAccountStats(ctx, userID, from)
	if err != nil {
		slog.WarnContext(ctx, "Failed to retrieve account stats for report", "userID", userID, common.ErrAttr(err))
		return reportCtx
	}

	orgs := make(map[int32]struct{})
	for _, stat := range stats {
		reportCtx.TotalRequests += uint64(stat.Count)
		orgs[stat.OrgID] = struct{}{}
	}
	reportCtx.OrgsCount = len(orgs)

	if count, err := j.Store.Impl().RetrieveUserPropertiesCount(ctx, userID); err == nil {
		reportCtx.PropertiesCount = int(count)
	}

	return reportCtx
}

func (j *ScheduleReportsJob) buildReportContextForUser(ctx context.Context, userID int32, period string, notifEmail string, userEmail string) (*email.UsageReportContext, *string) {
	reportCtx := j.buildReportContext(ctx, userID, period)

	var customEmail *string
	if len(notifEmail) > 0 && notifEmail != userEmail {
		customEmail = &notifEmail
	}

	return reportCtx, customEmail
}

func createWeeklyReportNotification(userID int32, tnow time.Time, reportCtx *email.UsageReportContext) *common.ScheduledNotification {
	year, week := tnow.ISOWeek()
	return &common.ScheduledNotification{
		ReferenceID:  weeklyReportReference(userID, year, week),
		UserID:       userID,
		Subject:      "[Private Captcha] Your weekly usage report",
		Data:         reportCtx,
		DateTime:     tnow,
		TemplateHash: email.WeeklyReportTemplate.Hash(),
		Persistent:   false,
		Condition:    common.EmptyNotificationCondition,
	}
}

func createMonthlyReportNotification(userID int32, tnow time.Time, reportCtx *email.UsageReportContext) *common.ScheduledNotification {
	return &common.ScheduledNotification{
		ReferenceID:  monthlyReportReference(userID, tnow.Year(), tnow.Month()),
		UserID:       userID,
		Subject:      "[Private Captcha] Your monthly usage report",
		Data:         reportCtx,
		DateTime:     tnow,
		TemplateHash: email.MonthlyReportTemplate.Hash(),
		Persistent:   false,
		Condition:    common.EmptyNotificationCondition,
	}
}

// RetrieveUsersWithWeeklyReport and RetrieveUsersWithMonthlyReport return
// *dbgen.GetUsersWithWeeklyReportRow and *dbgen.GetUsersWithMonthlyReportRow
// respectively - the fields UserID, NotificationsEmail, Email are used by
// the maintenance email sending job to deliver notifications.
