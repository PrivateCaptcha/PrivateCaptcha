package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/jpillora/backoff"
)

type ScheduleReportsJob struct {
	Store      db.Implementor
	TimeSeries common.TimeSeriesStore
	UsersLimit int32
}

type ScheduleReportsParams struct {
	UsersLimit int32 `json:"users_limit"`
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
	limit := j.UsersLimit
	if limit <= 0 {
		limit = 50
	}
	return &ScheduleReportsParams{
		UsersLimit: limit,
	}
}

func (j *ScheduleReportsJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*ScheduleReportsParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*ScheduleReportsParams)
	}

	tnow := time.Now().UTC()

	// Weekly reports: schedule on Mondays
	if tnow.Weekday() == time.Monday {
		if err := j.scheduleWeeklyReports(ctx, tnow, p.UsersLimit); err != nil {
			slog.ErrorContext(ctx, "Failed to schedule weekly reports", common.ErrAttr(err))
		}
	}

	// Monthly reports: schedule on the 1st of each month
	if tnow.Day() == 1 {
		if err := j.scheduleMonthlyReports(ctx, tnow, p.UsersLimit); err != nil {
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

func (j *ScheduleReportsJob) scheduleWeeklyReports(ctx context.Context, tnow time.Time, usersLimit int32) error {
	year, week := tnow.ISOWeek()
	fetchLimit := usersLimit + 1

	b := &backoff.Backoff{
		Min:    50 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	var offset int32
	for {
		users, err := j.Store.Impl().RetrieveUsersWithWeeklyReport(ctx, fetchLimit, offset)
		if err != nil {
			return err
		}

		hasMore := int32(len(users)) > usersLimit
		if hasMore {
			users = users[:usersLimit]
		}

		slog.InfoContext(ctx, "Scheduling weekly reports chunk", "count", len(users), "offset", offset)

		for _, user := range users {
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "Job context cancelled while scheduling weekly reports", common.ErrAttr(ctx.Err()))
				return ctx.Err()
			case <-time.After(b.Duration()):
			}

			j.scheduleWeeklyReportForUser(ctx, user.UserID, tnow, year, week)
		}

		if !hasMore {
			break
		}
		offset += usersLimit
	}

	return nil
}

func (j *ScheduleReportsJob) scheduleWeeklyReportForUser(ctx context.Context, userID int32, tnow time.Time, year, week int) {
	currentFrom := tnow.AddDate(0, 0, -7)
	prevFrom := tnow.AddDate(0, 0, -14)
	reportCtx := j.buildReportContext(ctx, userID, "weekly", prevFrom, currentFrom, tnow)

	notif := &common.ScheduledNotification{
		ReferenceID:  weeklyReportReference(userID, year, week),
		UserID:       userID,
		Subject:      "[Private Captcha] Your weekly usage report",
		Data:         reportCtx,
		DateTime:     tnow,
		TemplateHash: email.WeeklyReportTemplate.Hash(),
		Persistent:   false,
		Condition:    common.NotificationWithSubscription,
	}

	if _, err := j.Store.Impl().CreateUserNotification(ctx, notif); err != nil {
		slog.WarnContext(ctx, "Failed to create weekly report notification", "userID", userID, common.ErrAttr(err))
	}
}

func (j *ScheduleReportsJob) scheduleMonthlyReports(ctx context.Context, tnow time.Time, usersLimit int32) error {
	fetchLimit := usersLimit + 1

	b := &backoff.Backoff{
		Min:    50 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	var offset int32
	for {
		users, err := j.Store.Impl().RetrieveUsersWithMonthlyReport(ctx, fetchLimit, offset)
		if err != nil {
			return err
		}

		hasMore := int32(len(users)) > usersLimit
		if hasMore {
			users = users[:usersLimit]
		}

		slog.InfoContext(ctx, "Scheduling monthly reports chunk", "count", len(users), "offset", offset)

		for _, user := range users {
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "Job context cancelled while scheduling monthly reports", common.ErrAttr(ctx.Err()))
				return ctx.Err()
			case <-time.After(b.Duration()):
			}

			j.scheduleMonthlyReportForUser(ctx, user.UserID, tnow)
		}

		if !hasMore {
			break
		}
		offset += usersLimit
	}

	return nil
}

func (j *ScheduleReportsJob) scheduleMonthlyReportForUser(ctx context.Context, userID int32, tnow time.Time) {
	currentFrom := tnow.AddDate(0, -1, 0)
	prevFrom := tnow.AddDate(0, -2, 0)
	reportCtx := j.buildReportContext(ctx, userID, "monthly", prevFrom, currentFrom, tnow)

	notif := &common.ScheduledNotification{
		ReferenceID:  monthlyReportReference(userID, tnow.Year(), tnow.Month()),
		UserID:       userID,
		Subject:      "[Private Captcha] Your monthly usage report",
		Data:         reportCtx,
		DateTime:     tnow,
		TemplateHash: email.MonthlyReportTemplate.Hash(),
		Persistent:   false,
		Condition:    common.NotificationWithSubscription,
	}

	if _, err := j.Store.Impl().CreateUserNotification(ctx, notif); err != nil {
		slog.WarnContext(ctx, "Failed to create monthly report notification", "userID", userID, common.ErrAttr(err))
	}
}

func percentChange(current, previous uint64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return (float64(current) - float64(previous)) / float64(previous) * 100
}

func changeSign(change float64) string {
	if change > 0 {
		return "+"
	}
	return ""
}

const topPropertiesLimit = 5

func (j *ScheduleReportsJob) buildReportContext(ctx context.Context, userID int32, period string, prevFrom, currentFrom, to time.Time) *email.UsageReportContext {
	reportCtx := &email.UsageReportContext{
		Period:        period,
		DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint,
	}

	// Query current period property stats from ClickHouse
	currentStats, err := j.TimeSeries.RetrieveUserPropertyStatsBetween(ctx, userID, currentFrom, to, topPropertiesLimit)
	if err != nil {
		slog.WarnContext(ctx, "Failed to retrieve current period stats for report", "userID", userID, common.ErrAttr(err))
		return reportCtx
	}

	// Query previous period property stats from ClickHouse
	prevStats, err := j.TimeSeries.RetrieveUserPropertyStatsBetween(ctx, userID, prevFrom, currentFrom, topPropertiesLimit)
	if err != nil {
		slog.WarnContext(ctx, "Failed to retrieve previous period stats for report", "userID", userID, common.ErrAttr(err))
	}

	// Sum current totals from per-property stats
	for _, s := range currentStats {
		reportCtx.TotalRequests += s.Count
	}

	// Query verify counts for current and previous periods
	currentVerifies, err := j.TimeSeries.RetrieveUserVerifyCountBetween(ctx, userID, currentFrom, to)
	if err != nil {
		slog.WarnContext(ctx, "Failed to retrieve current verify count for report", "userID", userID, common.ErrAttr(err))
	}
	reportCtx.TotalVerifies = currentVerifies

	prevVerifies, err := j.TimeSeries.RetrieveUserVerifyCountBetween(ctx, userID, prevFrom, currentFrom)
	if err != nil {
		slog.WarnContext(ctx, "Failed to retrieve previous verify count for report", "userID", userID, common.ErrAttr(err))
	}
	reportCtx.PrevVerifies = prevVerifies

	// Sum previous request totals
	prevRequestsMap := make(map[int32]uint64, len(prevStats))
	for _, s := range prevStats {
		reportCtx.PrevRequests += s.Count
		prevRequestsMap[s.PropertyID] = s.Count
	}

	// Compute period-over-period changes
	reportCtx.RequestsChange = math.Abs(percentChange(reportCtx.TotalRequests, reportCtx.PrevRequests))
	reportCtx.RequestsSign = changeSign(percentChange(reportCtx.TotalRequests, reportCtx.PrevRequests))
	reportCtx.VerifiesChange = math.Abs(percentChange(reportCtx.TotalVerifies, reportCtx.PrevVerifies))
	reportCtx.VerifiesSign = changeSign(percentChange(reportCtx.TotalVerifies, reportCtx.PrevVerifies))

	// Verification rate
	if reportCtx.TotalRequests > 0 {
		reportCtx.VerificationRate = float64(reportCtx.TotalVerifies) / float64(reportCtx.TotalRequests) * 100
	}

	// Build top properties with batch property lookup
	if len(currentStats) > 0 && reportCtx.TotalRequests > 0 {
		batch := make(map[int32]uint, len(currentStats))
		for _, pc := range currentStats {
			batch[pc.PropertyID] = 0
		}

		properties, err := j.Store.Impl().RetrievePropertiesByID(ctx, batch)
		if err != nil {
			slog.WarnContext(ctx, "Failed to batch-retrieve properties for report", "userID", userID, common.ErrAttr(err))
			return reportCtx
		}

		propMap := make(map[int32]*dbgen.Property, len(properties))
		for _, p := range properties {
			propMap[p.ID] = p
		}

		topProperties := make([]email.PropertyStat, 0, len(currentStats))
		for _, pc := range currentStats {
			prop, ok := propMap[pc.PropertyID]
			if !ok {
				slog.DebugContext(ctx, "Skipping unknown property in report", "propID", pc.PropertyID)
				continue
			}

			percent := float64(pc.Count) / float64(reportCtx.TotalRequests) * 100
			prevCount := prevRequestsMap[pc.PropertyID]
			change := percentChange(pc.Count, prevCount)

			topProperties = append(topProperties, email.PropertyStat{
				Name:       prop.Name,
				Domain:     prop.Domain,
				Count:      pc.Count,
				Percent:    percent,
				PrevCount:  prevCount,
				Change:     math.Abs(change),
				ChangeSign: changeSign(change),
			})
		}
		reportCtx.TopProperties = topProperties
	}

	return reportCtx
}
