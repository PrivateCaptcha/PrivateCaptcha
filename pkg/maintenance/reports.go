package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
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
	colorGreen              = "#16a34a"
	colorRed                = "#dc2626"
	colorNeutral            = "#888888"
	floatEpsilon            = 1e-4
	processedMapCapacity    = 1000
)

type ScheduleReportsJob struct {
	Store       db.Implementor
	TimeSeries  common.TimeSeriesStore
	PlanService billing.PlanService
	UsersLimit  int32
	TTL         time.Duration

	mu        sync.Mutex
	processed map[int32]time.Time
}

type ScheduleReportsParams struct {
	UsersLimit int32 `json:"users_limit"`
}

var _ common.PeriodicJob = (*ScheduleReportsJob)(nil)

func NewScheduleReportsJob(store db.Implementor, ts common.TimeSeriesStore, planService billing.PlanService, usersLimit int32) *ScheduleReportsJob {
	return &ScheduleReportsJob{
		Store:       store,
		TimeSeries:  ts,
		PlanService: planService,
		UsersLimit:  usersLimit,
		TTL:         24 * time.Hour,
		processed:   make(map[int32]time.Time, processedMapCapacity),
	}
}

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

func (j *ScheduleReportsJob) isProcessed(userID int32, tnow time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	t, ok := j.processed[userID]
	if !ok {
		return false
	}

	return tnow.Sub(t) < j.TTL
}

func (j *ScheduleReportsJob) markProcessed(userID int32, tnow time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.processed[userID] = tnow
}

func (j *ScheduleReportsJob) gcProcessed(tnow time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()

	for id, t := range j.processed {
		if tnow.Sub(t) >= j.TTL {
			delete(j.processed, id)
		}
	}

	if len(j.processed) > processedMapCapacity {
		toDelete := len(j.processed) * 30 / 100
		deleted := 0
		for id := range j.processed {
			if deleted >= toDelete {
				break
			}
			delete(j.processed, id)
			deleted++
		}
	}
}

func (j *ScheduleReportsJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*ScheduleReportsParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*ScheduleReportsParams)
	}

	tnow := time.Now().UTC()

	j.gcProcessed(tnow)

	if tnow.Weekday() == time.Monday {
		if err := j.scheduleWeeklyReports(ctx, tnow, p.UsersLimit); err != nil {
			slog.ErrorContext(ctx, "Failed to schedule weekly reports", common.ErrAttr(err))
		}
	}

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

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
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
	for iteration := 0; iteration < maxPaginationIterations; iteration++ {
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

			if !j.PlanService.IsSubscriptionActive(user.SubscriptionStatus) {
				slog.DebugContext(ctx, "Skipping weekly report for user with inactive subscription", "userID", user.UserID, "status", user.SubscriptionStatus)
				continue
			}

			if j.isProcessed(user.UserID, tnow) {
				slog.DebugContext(ctx, "Skipping already processed user for weekly report", "userID", user.UserID)
				continue
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
	today := truncateDay(tnow)
	from := today.AddDate(0, 0, -14)
	mid := today.AddDate(0, 0, -7)

	reportCtx := BuildWeeklyReport(ctx, j.Store, j.TimeSeries, userID, from, mid, today)

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

	_, err := j.Store.Impl().CreateUserNotification(ctx, notif)
	if err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			j.markProcessed(userID, tnow)
			return
		}
		slog.WarnContext(ctx, "Failed to create weekly report notification", "userID", userID, common.ErrAttr(err))
		return
	}

	j.markProcessed(userID, tnow)
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
	for iteration := 0; iteration < maxPaginationIterations; iteration++ {
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

			if !j.PlanService.IsSubscriptionActive(user.SubscriptionStatus) {
				slog.DebugContext(ctx, "Skipping monthly report for user with inactive subscription", "userID", user.UserID, "status", user.SubscriptionStatus)
				continue
			}

			if j.isProcessed(user.UserID, tnow) {
				slog.DebugContext(ctx, "Skipping already processed user for monthly report", "userID", user.UserID)
				continue
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
	today := truncateDay(tnow)
	from := today.AddDate(0, -2, 0)
	mid := today.AddDate(0, -1, 0)

	reportCtx := BuildMonthlyReport(ctx, j.Store, j.TimeSeries, userID, from, mid, today)

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

	_, err := j.Store.Impl().CreateUserNotification(ctx, notif)
	if err != nil {
		if errors.Is(err, db.ErrAlreadyExists) {
			j.markProcessed(userID, tnow)
			return
		}
		slog.WarnContext(ctx, "Failed to create monthly report notification", "userID", userID, common.ErrAttr(err))
		return
	}

	j.markProcessed(userID, tnow)
}

// BuildWeeklyReport builds a complete weekly usage report for a user.
func BuildWeeklyReport(ctx context.Context, store db.Implementor, ts common.TimeSeriesStore, userID int32, from, mid, to time.Time) *email.UsageReportContext {
	report := &email.UsageReportContext{
		Period:        "weekly",
		DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint,
	}

	stats, err := ts.RetrieveWeeklyReportStats(ctx, userID, from, mid, to)
	if err != nil {
		slog.WarnContext(ctx, "Failed to retrieve weekly report stats", "userID", userID, common.ErrAttr(err))
		return report
	}

	fillTotals(report, stats)
	fillChanges(report, stats)
	fillTopProperties(ctx, store, report, stats)

	return report
}

// BuildMonthlyReport builds a complete monthly usage report for a user.
func BuildMonthlyReport(ctx context.Context, store db.Implementor, ts common.TimeSeriesStore, userID int32, from, mid, to time.Time) *email.UsageReportContext {
	report := &email.UsageReportContext{
		Period:        "monthly",
		DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint,
	}

	stats, err := ts.RetrieveMonthlyReportStats(ctx, userID, from, mid, to)
	if err != nil {
		slog.WarnContext(ctx, "Failed to retrieve monthly report stats", "userID", userID, common.ErrAttr(err))
		return report
	}

	fillTotals(report, stats)
	fillChanges(report, stats)
	fillTopProperties(ctx, store, report, stats)

	return report
}

func fillTotals(report *email.UsageReportContext, stats *common.UserReportStats) {
	report.TotalRequests = stats.TotalCurrentRequests
	report.PrevRequests = stats.TotalPrevRequests
	report.TotalVerifies = stats.TotalCurrentVerifies
	report.PrevVerifies = stats.TotalPrevVerifies
	if report.TotalRequests > 0 {
		report.VerificationRate = float64(report.TotalVerifies) / float64(report.TotalRequests) * 100
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
	if change > floatEpsilon {
		return "+"
	}
	if change < -floatEpsilon {
		return "-"
	}
	return ""
}

func changeColor(change float64) string {
	if change > floatEpsilon {
		return colorGreen
	}
	if change < -floatEpsilon {
		return colorRed
	}
	return colorNeutral
}

func fillChanges(report *email.UsageReportContext, stats *common.UserReportStats) {
	reqChange := percentChange(report.TotalRequests, report.PrevRequests)
	report.RequestsChange = math.Abs(reqChange)
	report.RequestsSign = changeSign(reqChange)
	report.RequestsColor = changeColor(reqChange)

	verChange := percentChange(report.TotalVerifies, report.PrevVerifies)
	report.VerifiesChange = math.Abs(verChange)
	report.VerifiesSign = changeSign(verChange)
	report.VerifiesColor = changeColor(verChange)
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

	topProperties := make([]email.PropertyStat, 0, len(props))
	for _, ps := range props {
		prop, ok := propMap[ps.PropertyID]
		if !ok {
			slog.DebugContext(ctx, "Skipping unknown property in report", "propID", ps.PropertyID)
			continue
		}

		percent := float64(ps.CurrentRequests) / float64(report.TotalRequests) * 100
		change := percentChange(ps.CurrentRequests, ps.PrevRequests)

		topProperties = append(topProperties, email.PropertyStat{
			Name:        prop.Name,
			Domain:      prop.Domain,
			Count:       ps.CurrentRequests,
			Percent:     percent,
			PrevCount:   ps.PrevRequests,
			Change:      math.Abs(change),
			ChangeSign:  changeSign(change),
			ChangeColor: changeColor(change),
		})
	}
	report.TopProperties = topProperties
}
