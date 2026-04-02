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
)

type ScheduleReportsJob struct {
	Store       db.Implementor
	TimeSeries  common.TimeSeriesStore
	PlanService billing.PlanService
	UsersLimit  int32

	mu        sync.Mutex
	processed map[int32]time.Time
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

func (j *ScheduleReportsJob) isProcessedToday(userID int32, today time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.processed == nil {
		j.processed = make(map[int32]time.Time)
	}

	t, ok := j.processed[userID]
	if !ok {
		return false
	}

	return t.Year() == today.Year() && t.YearDay() == today.YearDay()
}

func (j *ScheduleReportsJob) markProcessed(userID int32, today time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.processed == nil {
		j.processed = make(map[int32]time.Time)
	}

	j.processed[userID] = today
}

func (j *ScheduleReportsJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*ScheduleReportsParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*ScheduleReportsParams)
	}

	tnow := time.Now().UTC()

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

			if j.isProcessedToday(user.UserID, tnow) {
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
	reportCtx := BuildUsageReport(ctx, j.Store, j.TimeSeries, userID, false, tnow.AddDate(0, 0, -14), tnow.AddDate(0, 0, -7), tnow)

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

			if j.isProcessedToday(user.UserID, tnow) {
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
	reportCtx := BuildUsageReport(ctx, j.Store, j.TimeSeries, userID, true, tnow.AddDate(0, -2, 0), tnow.AddDate(0, -1, 0), tnow)

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

// UsageReportBuilder constructs a UsageReportContext step by step.
type UsageReportBuilder struct {
	ctx        context.Context
	store      db.Implementor
	timeSeries common.TimeSeriesStore
	userID     int32
	monthly    bool
	from       time.Time
	mid        time.Time
	to         time.Time
	stats      *common.UserReportStats
	reportCtx  *email.UsageReportContext
}

func NewUsageReportBuilder(ctx context.Context, store db.Implementor, ts common.TimeSeriesStore, userID int32, monthly bool, from, mid, to time.Time) *UsageReportBuilder {
	period := "weekly"
	if monthly {
		period = "monthly"
	}
	return &UsageReportBuilder{
		ctx:        ctx,
		store:      store,
		timeSeries: ts,
		userID:     userID,
		monthly:    monthly,
		from:       from,
		mid:        mid,
		to:         to,
		reportCtx: &email.UsageReportContext{
			Period:        period,
			DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint,
		},
	}
}

// BuildUsageReport is a convenience function that runs the full builder pipeline.
func BuildUsageReport(ctx context.Context, store db.Implementor, ts common.TimeSeriesStore, userID int32, monthly bool, from, mid, to time.Time) *email.UsageReportContext {
	b := NewUsageReportBuilder(ctx, store, ts, userID, monthly, from, mid, to)
	b.FetchStats()
	b.ComputeTotals()
	b.ComputeChanges()
	b.BuildTopProperties()
	return b.Build()
}

func (b *UsageReportBuilder) FetchStats() {
	stats, err := b.timeSeries.RetrieveUserReportStats(b.ctx, b.userID, b.from, b.mid, b.to, b.monthly)
	if err != nil {
		slog.WarnContext(b.ctx, "Failed to retrieve report stats", "userID", b.userID, "monthly", b.monthly, common.ErrAttr(err))
		return
	}
	b.stats = stats
}

func (b *UsageReportBuilder) ComputeTotals() {
	if b.stats == nil {
		return
	}
	b.reportCtx.TotalRequests = b.stats.TotalCurrentRequests
	b.reportCtx.PrevRequests = b.stats.TotalPrevRequests
	b.reportCtx.TotalVerifies = b.stats.TotalCurrentVerifies
	b.reportCtx.PrevVerifies = b.stats.TotalPrevVerifies
	if b.reportCtx.TotalRequests > 0 {
		b.reportCtx.VerificationRate = float64(b.reportCtx.TotalVerifies) / float64(b.reportCtx.TotalRequests) * 100
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

func changeColor(change float64) string {
	if change > 0 {
		return colorGreen
	}
	if change < 0 {
		return colorRed
	}
	return colorNeutral
}

func (b *UsageReportBuilder) ComputeChanges() {
	if b.stats == nil {
		return
	}
	reqChange := percentChange(b.reportCtx.TotalRequests, b.reportCtx.PrevRequests)
	b.reportCtx.RequestsChange = math.Abs(reqChange)
	b.reportCtx.RequestsSign = changeSign(reqChange)
	b.reportCtx.RequestsColor = changeColor(reqChange)

	verChange := percentChange(b.reportCtx.TotalVerifies, b.reportCtx.PrevVerifies)
	b.reportCtx.VerifiesChange = math.Abs(verChange)
	b.reportCtx.VerifiesSign = changeSign(verChange)
	b.reportCtx.VerifiesColor = changeColor(verChange)
}

func (b *UsageReportBuilder) BuildTopProperties() {
	if b.stats == nil || len(b.stats.Properties) == 0 || b.reportCtx.TotalRequests == 0 {
		return
	}

	props := b.stats.Properties
	if len(props) > topPropertiesLimit {
		props = props[:topPropertiesLimit]
	}

	batch := make(map[int32]uint, len(props))
	for _, ps := range props {
		batch[ps.PropertyID] = 0
	}

	properties, err := b.store.Impl().RetrievePropertiesByID(b.ctx, batch)
	if err != nil {
		slog.WarnContext(b.ctx, "Failed to batch-retrieve properties for report", "userID", b.userID, common.ErrAttr(err))
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
			slog.DebugContext(b.ctx, "Skipping unknown property in report", "propID", ps.PropertyID)
			continue
		}

		percent := float64(ps.CurrentRequests) / float64(b.reportCtx.TotalRequests) * 100
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
	b.reportCtx.TopProperties = topProperties
}

func (b *UsageReportBuilder) Build() *email.UsageReportContext {
	return b.reportCtx
}
