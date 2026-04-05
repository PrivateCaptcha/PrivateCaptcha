package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/jpillora/backoff"
)

const (
	maxPaginationIterations = 100
	topPropertiesLimit      = 5
	floatEpsilon            = 1e-4

	WeeklyReferencePrefix  = "report/weekly/"
	MonthlyReferencePrefix = "report/monthly/"
)

type ScheduleReportsJob struct {
	Store       db.Implementor
	TimeSeries  common.TimeSeriesStore
	PlanService billing.PlanService
	Stage       string
	UsersLimit  int32
}

type ScheduleReportsParams struct {
	UsersLimit int32  `json:"users_limit,omitempty"`
	UserID     int32  `json:"user_id,omitempty"`
	UserEmail  string `json:"user_email,omitempty"`
	Weekly     bool   `json:"weekly,omitempty"`
	Monthly    bool   `json:"monthly,omitempty"`
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
		Weekly:     true,
		Monthly:    true,
	}
}

func (j *ScheduleReportsJob) RunOnce(ctx context.Context, params any) error {
	return j.RunOnceAt(ctx, params, time.Now().UTC())
}

func (j *ScheduleReportsJob) RunOnceAt(ctx context.Context, params any, tnow time.Time) error {
	p, ok := params.(*ScheduleReportsParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*ScheduleReportsParams)
	}

	if p.UserID > 0 {
		slog.DebugContext(ctx, "Processing reports for a single user", "userID", p.UserID, "weekly", p.Weekly, "monthly", p.Monthly)

		var errs []error
		if p.Weekly {
			if err := j.scheduleWeeklyReportForUser(ctx, p.UserID, &p.UserEmail, tnow, 0); err != nil {
				errs = append(errs, err)
			}
		}
		if p.Monthly {
			if err := j.scheduleMonthlyReportForUser(ctx, p.UserID, &p.UserEmail, tnow, 0); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}

	slog.DebugContext(ctx, "Processing reports for users", "limit", p.UsersLimit, "weekly", p.Weekly, "monthly", p.Monthly,
		"weekday", tnow.Weekday(), "dayOfTheMonth", tnow.Day())

	if p.Weekly && (tnow.Weekday() == time.Monday) {
		if err := j.scheduleWeeklyReports(ctx, tnow, p.UsersLimit); err != nil {
			slog.ErrorContext(ctx, "Failed to schedule weekly reports", common.ErrAttr(err))
		}
	}

	if p.Monthly && (tnow.Day() == 1) {
		if err := j.scheduleMonthlyReports(ctx, tnow, p.UsersLimit); err != nil {
			slog.ErrorContext(ctx, "Failed to schedule monthly reports", common.ErrAttr(err))
		}
	}

	return nil
}

func weeklyReportReference(userID int32, year int, week int) string {
	return fmt.Sprintf("%s%d%s", WeeklyReferencePrefix, userID, weeklyReferenceSuffix(year, week))
}

func weeklyReferenceSuffix(year int, week int) string {
	return fmt.Sprintf("/%d/%d", year, week)
}

func monthlyReportReference(userID int32, year int, month time.Month) string {
	return fmt.Sprintf("%s%d%s", MonthlyReferencePrefix, userID, monthlyReferenceSuffix(year, month))
}

func monthlyReferenceSuffix(year int, month time.Month) string {
	return fmt.Sprintf("/%d/%d", year, month)
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func (j *ScheduleReportsJob) scheduleWeeklyReports(ctx context.Context, tnow time.Time, usersLimit int32) error {
	year, week := tnow.ISOWeek()
	fetchLimit := usersLimit + 1
	refSuffix := weeklyReferenceSuffix(year, week)

	b := &backoff.Backoff{
		Min:    50 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	var lastSeenUserID int32
	for iteration := 0; iteration < maxPaginationIterations; iteration++ {
		users, err := j.Store.Impl().RetrieveUsersWithPendingWeeklyReport(ctx, fetchLimit, lastSeenUserID, WeeklyReferencePrefix, refSuffix)
		if err != nil {
			return err
		}

		hasMore := int32(len(users)) > usersLimit
		if hasMore {
			users = users[:usersLimit]
		}

		slog.InfoContext(ctx, "Scheduling weekly reports chunk", "count", len(users), "lastSeenUserID", lastSeenUserID)

		for _, user := range users {
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "Job context cancelled while scheduling weekly reports", common.ErrAttr(ctx.Err()))
				return ctx.Err()
			case <-time.After(b.Duration()):
			}

			if !j.PlanService.IsSubscriptionActive(user.SubscriptionStatus) {
				slog.DebugContext(ctx, "Skipping weekly report for user with inactive subscription", "userID", user.UserID, "status", user.SubscriptionStatus)
				continue
			}

			var emailTo *string
			if user.NotificationsEmail.Valid {
				emailTo = &user.NotificationsEmail.String
			}

			var accountLimit uint64
			if user.ExternalProductID != "" && user.ExternalPriceID != "" {
				plan, err := j.PlanService.FindPlan(user.ExternalProductID, user.ExternalPriceID, j.Stage, true)
				if err != nil {
					plan, err = j.PlanService.FindPlan(user.ExternalProductID, user.ExternalPriceID, j.Stage, false)
				}
				if err == nil && plan.RequestsLimit() > 0 {
					accountLimit = uint64(plan.RequestsLimit())
				}
			}

			// single user report failure shouldn't abort this
			_ = j.scheduleWeeklyReportForUser(ctx, user.UserID, emailTo, tnow, accountLimit)

			lastSeenUserID = user.UserID
		}

		if !hasMore {
			break
		}
	}

	return nil
}

func (j *ScheduleReportsJob) scheduleWeeklyReportForUser(ctx context.Context, userID int32, emailTo *string, tnow time.Time, accountLimit uint64) error {
	today := truncateDay(tnow)
	from := today.AddDate(0, 0, -14)
	mid := today.AddDate(0, 0, -7)

	reportCtx, err := BuildWeeklyReport(ctx, j.Store, j.TimeSeries, userID, from, mid, today)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to build weekly report", "userID", userID, common.ErrAttr(err))
		return err
	}

	reportCtx.AccountLimit = accountLimit

	year, week := tnow.ISOWeek()

	notif := &common.ScheduledNotification{
		ReferenceID:  weeklyReportReference(userID, year, week),
		UserID:       userID,
		Subject:      "[Private Captcha] Your weekly usage report",
		Data:         reportCtx,
		DateTime:     tnow,
		TemplateHash: email.UsageReportTemplate.Hash(),
		Persistent:   false,
		Condition:    common.NotificationWithSubscription,
	}

	if (emailTo != nil) && (len(*emailTo) > 0) {
		notif.EmailTo = emailTo
	}

	_, err = j.Store.Impl().CreateUserNotification(ctx, notif)
	if err != nil {
		if !errors.Is(err, db.ErrAlreadyExists) {
			slog.WarnContext(ctx, "Failed to create weekly report notification", "userID", userID, common.ErrAttr(err))
			return err
		}
	}
	return nil
}

func (j *ScheduleReportsJob) scheduleMonthlyReports(ctx context.Context, tnow time.Time, usersLimit int32) error {
	fetchLimit := usersLimit + 1
	refSuffix := monthlyReferenceSuffix(tnow.Year(), tnow.Month())

	b := &backoff.Backoff{
		Min:    50 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	var lastSeenUserID int32
	for iteration := 0; iteration < maxPaginationIterations; iteration++ {
		users, err := j.Store.Impl().RetrieveUsersWithPendingMonthlyReport(ctx, fetchLimit, lastSeenUserID, MonthlyReferencePrefix, refSuffix)
		if err != nil {
			return err
		}

		hasMore := int32(len(users)) > usersLimit
		if hasMore {
			users = users[:usersLimit]
		}

		slog.InfoContext(ctx, "Scheduling monthly reports chunk", "count", len(users), "lastSeenUserID", lastSeenUserID)

		for _, user := range users {
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "Job context cancelled while scheduling monthly reports", common.ErrAttr(ctx.Err()))
				return ctx.Err()
			case <-time.After(b.Duration()):
			}

			if !j.PlanService.IsSubscriptionActive(user.SubscriptionStatus) {
				slog.DebugContext(ctx, "Skipping monthly report for user with inactive subscription", "userID", user.UserID, "status", user.SubscriptionStatus)
				continue
			}

			var emailTo *string
			if user.NotificationsEmail.Valid {
				emailTo = &user.NotificationsEmail.String
			}

			var accountLimit uint64
			if user.ExternalProductID != "" && user.ExternalPriceID != "" {
				plan, err := j.PlanService.FindPlan(user.ExternalProductID, user.ExternalPriceID, j.Stage, true)
				if err != nil {
					plan, err = j.PlanService.FindPlan(user.ExternalProductID, user.ExternalPriceID, j.Stage, false)
				}
				if err == nil && plan.RequestsLimit() > 0 {
					accountLimit = uint64(plan.RequestsLimit())
				}
			}

			// single user report failure shouldn't abort this
			_ = j.scheduleMonthlyReportForUser(ctx, user.UserID, emailTo, tnow, accountLimit)

			lastSeenUserID = user.UserID
		}

		if !hasMore {
			break
		}
	}

	return nil
}

func (j *ScheduleReportsJob) scheduleMonthlyReportForUser(ctx context.Context, userID int32, emailTo *string, tnow time.Time, accountLimit uint64) error {
	today := truncateDay(tnow)
	from := today.AddDate(0, -2, 0)
	mid := today.AddDate(0, -1, 0)

	reportCtx, err := BuildMonthlyReport(ctx, j.Store, j.TimeSeries, userID, from, mid, today)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to build monthly report", "userID", userID, common.ErrAttr(err))
		return err
	}

	reportCtx.AccountLimit = accountLimit

	notif := &common.ScheduledNotification{
		ReferenceID:  monthlyReportReference(userID, tnow.Year(), tnow.Month()),
		UserID:       userID,
		Subject:      "[Private Captcha] Your monthly usage report",
		Data:         reportCtx,
		DateTime:     tnow,
		TemplateHash: email.UsageReportTemplate.Hash(),
		Persistent:   false,
		Condition:    common.NotificationWithSubscription,
	}

	if (emailTo != nil) && (len(*emailTo) > 0) {
		notif.EmailTo = emailTo
	}

	if _, err := j.Store.Impl().CreateUserNotification(ctx, notif); err != nil {
		if !errors.Is(err, db.ErrAlreadyExists) {
			slog.WarnContext(ctx, "Failed to create monthly report notification", "userID", userID, common.ErrAttr(err))
			return err
		}
	}

	return nil
}

// BuildWeeklyReport builds a complete weekly usage report for a user.
func BuildWeeklyReport(ctx context.Context, store db.Implementor, ts common.TimeSeriesStore, userID int32, from, mid, to time.Time) (*email.UsageReportContext, error) {
	report := &email.UsageReportContext{
		Period:        "weekly",
		PeriodDate:    to.Format("02 Jan 2006"),
		DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint,
	}

	stats, err := ts.RetrieveWeeklyReportStats(ctx, userID, from, mid, to)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve weekly report stats", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	fillTotals(report, stats)
	fillChanges(report, stats)
	fillTopProperties(ctx, store, report, stats)

	return report, nil
}

// BuildMonthlyReport builds a complete monthly usage report for a user.
func BuildMonthlyReport(ctx context.Context, store db.Implementor, ts common.TimeSeriesStore, userID int32, from, mid, to time.Time) (*email.UsageReportContext, error) {
	report := &email.UsageReportContext{
		Period:        "monthly",
		PeriodDate:    to.Format("Jan 2006"),
		DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint,
	}

	stats, err := ts.RetrieveMonthlyReportStats(ctx, userID, from, mid, to)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve monthly report stats", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	fillTotals(report, stats)
	fillChanges(report, stats)
	fillTopProperties(ctx, store, report, stats)

	return report, nil
}

func fillTotals(report *email.UsageReportContext, stats *common.UserReportStats) {
	report.TotalRequests = stats.TotalCurrentRequests
	report.PrevRequests = stats.TotalPrevRequests
	report.TotalVerifies = stats.TotalCurrentVerifies
	report.PrevVerifies = stats.TotalPrevVerifies
	report.VerificationRate = verificationRate(report.TotalRequests, report.TotalVerifies)
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

func percentChangeFloat(current, previous float64) float64 {
	if math.Abs(previous) < floatEpsilon {
		if math.Abs(current) < floatEpsilon {
			return 0
		}
		return 100
	}
	return (current - previous) / previous * 100
}

func verificationRate(totalRequests, totalVerifies uint64) float64 {
	if totalRequests == 0 {
		return 0
	}
	return float64(totalVerifies) / float64(totalRequests) * 100
}

func fillChanges(report *email.UsageReportContext, stats *common.UserReportStats) {
	report.RequestsChange = percentChange(report.TotalRequests, report.PrevRequests)
	report.VerifiesChange = percentChange(report.TotalVerifies, report.PrevVerifies)
	report.VerificationRateChange = percentChangeFloat(
		report.VerificationRate,
		verificationRate(report.PrevRequests, report.PrevVerifies),
	)
}

func fillTopProperties(ctx context.Context, store db.Implementor, report *email.UsageReportContext, stats *common.UserReportStats) {
	if len(stats.Properties) == 0 || report.TotalRequests == 0 {
		return
	}

	props := stats.Properties
	if len(props) > topPropertiesLimit {
		props = props[:topPropertiesLimit]
	}

	batch := make(map[int32]uint, len(props))
	for _, ps := range props {
		batch[ps.PropertyID] = 0
	}

	properties, err := store.Impl().RetrievePropertiesByID(ctx, batch)
	if err != nil {
		slog.WarnContext(ctx, "Failed to batch-retrieve properties for report", common.ErrAttr(err))
		return
	}

	propMap := make(map[int32]*dbgen.Property, len(properties))
	for _, p := range properties {
		propMap[p.ID] = p
	}

	topProperties := make([]*email.PropertyStat, 0, len(props))
	for _, ps := range props {
		prop, ok := propMap[ps.PropertyID]
		if !ok {
			slog.DebugContext(ctx, "Skipping unknown property in report", "propID", ps.PropertyID)
			continue
		}

		percent := float64(ps.CurrentRequests) / float64(report.TotalRequests) * 100
		change := percentChange(ps.CurrentRequests, ps.PrevRequests)

		pStat := &email.PropertyStat{
			Name:      prop.Name,
			Domain:    prop.Domain,
			Count:     ps.CurrentRequests,
			Percent:   percent,
			Change:    change,
			Alternate: len(topProperties)%2 == 1,
		}
		if prop.AllowSubdomains {
			pStat.Domain = "*." + prop.Domain
		}

		topProperties = append(topProperties, pStat)
	}
	report.TopProperties = topProperties
}
