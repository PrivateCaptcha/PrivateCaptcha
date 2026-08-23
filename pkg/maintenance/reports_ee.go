//go:build enterprise

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/jpillora/backoff"
)

const (
	reportEmailUTM = "utm_medium=email&utm_source=report"
)

var (
	errAccountStats = errors.New("account level stats are missing")
)

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
			if err := j.scheduleWeeklyReportForUser(ctx, p.UserID, &p.UserEmail, tnow, 0, nil); err != nil {
				errs = append(errs, err)
			}
		}
		if p.Monthly {
			if err := j.scheduleMonthlyReportForUser(ctx, p.UserID, &p.UserEmail, tnow, 0, nil); err != nil {
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

func weeklyReportPeriod(tnow time.Time) (time.Time, time.Time, time.Time) {
	to := truncateDay(tnow)
	return to.AddDate(0, 0, -14), to.AddDate(0, 0, -7), to
}

func monthlyReportPeriod(tnow time.Time) (time.Time, time.Time, time.Time) {
	to := truncateDay(tnow)
	return to.AddDate(0, -2, 0), to.AddDate(0, -1, 0), to
}

func (j *ScheduleReportsJob) retrieveRequestLimit(ctx context.Context, productID, priceID string, status string) uint64 {
	if (len(productID) == 0) || (len(priceID) == 0) {
		return 0
	}

	isTrialing := j.PlanService.IsSubscriptionTrialing(status)
	if isTrialing {
		if trialPlan := j.PlanService.GetInternalTrialPlan(); trialPlan.Equals(productID, priceID) {
			return uint64(trialPlan.RequestsLimit(true /*isTrial*/))
		}

		return 0
	}

	var accountLimit uint64 = 0

	if plan, err := j.PlanService.FindPlan(productID, priceID, j.Stage, false /*internal*/); err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan", "productID", productID, "priceID", priceID, common.ErrAttr(err))
	} else {
		accountLimit = uint64(plan.RequestsLimit(isTrialing))
	}

	return accountLimit
}

func userWeeklyReportsRowsToIDs(rows []*dbgen.GetUsersWithPendingWeeklyReportRow) []int32 {
	userIDs := make([]int32, len(rows))
	for i, user := range rows {
		userIDs[i] = user.UserID
	}

	return userIDs
}

func userMonthlyReportsRowsToIDs(rows []*dbgen.GetUsersWithPendingMonthlyReportRow) []int32 {
	userIDs := make([]int32, len(rows))
	for i, user := range rows {
		userIDs[i] = user.UserID
	}

	return userIDs
}

func (j *ScheduleReportsJob) scheduleWeeklyReports(ctx context.Context, tnow time.Time, usersLimit int32) error {
	year, week := tnow.ISOWeek()
	fetchLimit := usersLimit + 1
	refSuffix := weeklyReferenceSuffix(year, week)
	from, mid, to := weeklyReportPeriod(tnow)

	b := &backoff.Backoff{
		Min:    50 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	var lastSeenUserID int32
	for iteration := 0; iteration < maxPaginationIterations; iteration++ {
		users, err := j.Store.Impl().RetrieveUsersWithPendingWeeklyReport(ctx, fetchLimit, lastSeenUserID, WeeklyReferencePrefix, refSuffix, j.PlanService.ExpiredTrialStatus())
		if err != nil {
			return err
		}

		hasMore := int32(len(users)) > usersLimit
		if hasMore {
			users = users[:usersLimit]
		}
		var accountStats map[int32]*common.UserReportAccountStats
		if len(users) > 0 {
			userIDs := userWeeklyReportsRowsToIDs(users)
			accountStats, err = j.TimeSeries.RetrieveWeeklyAccountReportStats(ctx, userIDs, from, mid, to)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to batch-retrieve weekly account report stats", "users", len(userIDs), common.ErrAttr(err))
				accountStats = nil
				// we don't return because we retry on per-user basis
			}
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

			accountLimit := j.retrieveRequestLimit(ctx, user.ExternalProductID, user.ExternalPriceID, user.SubscriptionStatus)
			userAccountStats := accountStats[user.UserID]
			if userAccountStats == nil {
				slog.WarnContext(ctx, "User is missing from weekly account report stats batch", "userID", user.UserID)
			}

			// single user report failure shouldn't abort this
			_ = j.scheduleWeeklyReportForUser(ctx, user.UserID, emailTo, tnow, accountLimit, userAccountStats)

			lastSeenUserID = user.UserID
		}

		if !hasMore {
			break
		}
	}

	return nil
}

func (j *ScheduleReportsJob) scheduleWeeklyReportForUser(ctx context.Context, userID int32, emailTo *string, tnow time.Time, accountLimit uint64, accountStats *common.UserReportAccountStats) error {
	from, mid, to := weeklyReportPeriod(tnow)

	reportCtx, err := j.buildWeeklyReport(ctx, userID, from, mid, to, accountStats)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to build weekly report", "userID", userID, common.ErrAttr(err))
		return err
	}

	reportCtx.AccountLimit = accountLimit

	year, week := tnow.ISOWeek()
	persistUntil := tnow.AddDate(0, 1, 0)

	notif := &common.ScheduledNotification{
		ReferenceID:  weeklyReportReference(userID, year, week),
		UserID:       userID,
		Subject:      "[Private Captcha] Your weekly usage report",
		Data:         reportCtx,
		DateTime:     tnow,
		TemplateHash: email.UsageReportTemplate.Hash(),
		PersistUntil: &persistUntil,
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
	from, mid, to := monthlyReportPeriod(tnow)

	b := &backoff.Backoff{
		Min:    50 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	var lastSeenUserID int32
	for iteration := 0; iteration < maxPaginationIterations; iteration++ {
		users, err := j.Store.Impl().RetrieveUsersWithPendingMonthlyReport(ctx, fetchLimit, lastSeenUserID, MonthlyReferencePrefix, refSuffix, j.PlanService.ExpiredTrialStatus())
		if err != nil {
			return err
		}

		hasMore := int32(len(users)) > usersLimit
		if hasMore {
			users = users[:usersLimit]
		}
		var accountStats map[int32]*common.UserReportAccountStats
		if len(users) > 0 {
			userIDs := userMonthlyReportsRowsToIDs(users)
			accountStats, err = j.TimeSeries.RetrieveMonthlyAccountReportStats(ctx, userIDs, from, mid, to)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to batch-retrieve monthly account report stats", "users", len(userIDs), common.ErrAttr(err))
				accountStats = nil
				// we don't return because we retry on per-user basis
			}
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

			accountLimit := j.retrieveRequestLimit(ctx, user.ExternalProductID, user.ExternalPriceID, user.SubscriptionStatus)
			userAccountStats := accountStats[user.UserID]
			if userAccountStats == nil {
				slog.WarnContext(ctx, "User is missing from monthly account report stats batch", "userID", user.UserID)
			}

			// single user report failure shouldn't abort this
			_ = j.scheduleMonthlyReportForUser(ctx, user.UserID, emailTo, tnow, accountLimit, userAccountStats)

			lastSeenUserID = user.UserID
		}

		if !hasMore {
			break
		}
	}

	return nil
}

func (j *ScheduleReportsJob) scheduleMonthlyReportForUser(ctx context.Context, userID int32, emailTo *string, tnow time.Time, accountLimit uint64, accountStats *common.UserReportAccountStats) error {
	from, mid, to := monthlyReportPeriod(tnow)

	reportCtx, err := j.buildMonthlyReport(ctx, userID, from, mid, to, accountStats)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to build monthly report", "userID", userID, common.ErrAttr(err))
		return err
	}

	reportCtx.AccountLimit = accountLimit

	persistUntil := tnow.AddDate(0, 1, 0)

	notif := &common.ScheduledNotification{
		ReferenceID:  monthlyReportReference(userID, tnow.Year(), tnow.Month()),
		UserID:       userID,
		Subject:      "[Private Captcha] Your monthly usage report",
		Data:         reportCtx,
		DateTime:     tnow,
		TemplateHash: email.UsageReportTemplate.Hash(),
		PersistUntil: &persistUntil,
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

func (j *ScheduleReportsJob) BuildWeeklyReport(ctx context.Context, userID int32, from, mid, to time.Time) (*email.UsageReportContext, error) {
	return j.buildWeeklyReport(ctx, userID, from, mid, to, nil)
}

func (j *ScheduleReportsJob) buildWeeklyReport(ctx context.Context, userID int32, from, mid, to time.Time, accountStats *common.UserReportAccountStats) (*email.UsageReportContext, error) {
	utm := fmt.Sprintf("%s&utm_campaign=weekly_%s", reportEmailUTM, strings.ToLower(to.Format("02_Jan_2006")))

	report := &email.UsageReportContext{
		Period:        "weekly",
		PeriodDate:    to.Format("02 Jan 2006"),
		DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint + "&" + utm,
		UTM:           utm,
	}
	if accountStats == nil {
		statsByUser, err := j.TimeSeries.RetrieveWeeklyAccountReportStats(ctx, []int32{userID}, from, mid, to)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve weekly account report stats", "userID", userID, common.ErrAttr(err))
			return nil, err
		}
		accountStats = statsByUser[userID]
		if accountStats == nil {
			slog.ErrorContext(ctx, "Failed to find weekly account report stats", "userID", userID)
			return nil, errAccountStats
		}
	}

	stats, err := j.TimeSeries.RetrieveWeeklyPropertiesReportStats(ctx, userID, from, mid, to, userReportOptions(weeklySecurityEventsLimit))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve weekly report stats", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	fillTotals(report, accountStats)
	fillChanges(report)
	fillPropertyReportDetails(ctx, j.Store, report, stats, j.PortalURL, j.IDHasher)

	if formStats, err := j.TimeSeries.RetrieveWeeklyFormsReportStats(ctx, userID, from, mid, to); err == nil {
		fillFormTotals(report, formStats)
		fillFormChanges(report, formStats)
		fillTopForms(ctx, j.Store, report, formStats, j.PortalURL, j.IDHasher)
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve weekly forms report stats", "userID", userID, common.ErrAttr(err))
	}

	return report, nil
}

func (j *ScheduleReportsJob) BuildMonthlyReport(ctx context.Context, userID int32, from, mid, to time.Time) (*email.UsageReportContext, error) {
	return j.buildMonthlyReport(ctx, userID, from, mid, to, nil)
}

func (j *ScheduleReportsJob) buildMonthlyReport(ctx context.Context, userID int32, from, mid, to time.Time, accountStats *common.UserReportAccountStats) (*email.UsageReportContext, error) {
	utm := fmt.Sprintf("%s&utm_campaign=monthly_%s", reportEmailUTM, strings.ToLower(to.Format("Jan_2006")))

	report := &email.UsageReportContext{
		Period:        "monthly",
		PeriodDate:    to.Format("Jan 2006"),
		DashboardPath: common.SettingsEndpoint + "?tab=" + common.UsageEndpoint + "&" + utm,
		UTM:           utm,
	}
	if accountStats == nil {
		statsByUser, err := j.TimeSeries.RetrieveMonthlyAccountReportStats(ctx, []int32{userID}, from, mid, to)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve monthly account report stats", "userID", userID, common.ErrAttr(err))
			return nil, err
		}
		accountStats = statsByUser[userID]
		if accountStats == nil {
			slog.ErrorContext(ctx, "Failed to find monthly account report stats", "userID", userID)
			return nil, errAccountStats
		}
	}

	stats, err := j.TimeSeries.RetrieveMonthlyPropertiesReportStats(ctx, userID, from, mid, to, userReportOptions(monthlySecurityEventsLimit))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve monthly report stats", "userID", userID, common.ErrAttr(err))
		return nil, err
	}

	fillTotals(report, accountStats)
	fillChanges(report)
	fillPropertyReportDetails(ctx, j.Store, report, stats, j.PortalURL, j.IDHasher)

	if formStats, err := j.TimeSeries.RetrieveMonthlyFormsReportStats(ctx, userID, from, mid, to); err == nil {
		fillFormTotals(report, formStats)
		fillFormChanges(report, formStats)
		fillTopForms(ctx, j.Store, report, formStats, j.PortalURL, j.IDHasher)
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve monthly forms report stats", "userID", userID, common.ErrAttr(err))
	}

	return report, nil
}

func fillTotals(report *email.UsageReportContext, stats *common.UserReportAccountStats) {
	report.TotalRequests = stats.CurrentRequests
	report.PrevRequests = stats.PrevRequests
	report.TotalVerifies = stats.CurrentVerifies
	report.PrevVerifies = stats.PrevVerifies
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

func formErrorRate(totalSubmissions, totalErrors uint64) float64 {
	if totalSubmissions == 0 {
		return 0
	}
	return float64(totalErrors) / float64(totalSubmissions) * 100
}

func fillChanges(report *email.UsageReportContext) {
	report.RequestsChange = percentChange(report.TotalRequests, report.PrevRequests)
	report.VerifiesChange = percentChange(report.TotalVerifies, report.PrevVerifies)
	report.VerificationRateChange = percentChangeFloat(
		report.VerificationRate,
		verificationRate(report.PrevRequests, report.PrevVerifies),
	)
}

func fillFormTotals(report *email.UsageReportContext, stats *common.UserFormsReportStats) {
	if stats == nil {
		return
	}

	report.TotalFormSubmissions = stats.TotalCurrentSubmissions
	report.PrevFormSubmissions = stats.TotalPrevSubmissions
	report.TotalFormErrors = stats.TotalCurrentErrors
	report.PrevFormErrors = stats.TotalPrevErrors
	report.FormErrorRate = formErrorRate(report.TotalFormSubmissions, report.TotalFormErrors)
}

func fillFormChanges(report *email.UsageReportContext, stats *common.UserFormsReportStats) {
	if stats == nil {
		return
	}

	report.FormSubmissionsChange = percentChange(report.TotalFormSubmissions, report.PrevFormSubmissions)
	report.FormErrorsChange = percentChange(report.TotalFormErrors, report.PrevFormErrors)
	report.FormErrorRateChange = percentChangeFloat(
		report.FormErrorRate,
		formErrorRate(report.PrevFormSubmissions, report.PrevFormErrors),
	)
}

func fillTopForms(ctx context.Context, store db.Implementor, report *email.UsageReportContext, stats *common.UserFormsReportStats, portalURL string, hasher common.IdentifierHasher) {
	if (stats == nil) || (len(stats.Forms) == 0) || (report.TotalFormSubmissions == 0) {
		return
	}

	formsStats := stats.Forms
	if len(formsStats) > topPropertiesLimit {
		formsStats = formsStats[:topPropertiesLimit]
	}

	batch := make(map[int32]uint, len(formsStats))
	for _, fs := range formsStats {
		batch[fs.FormID] = 0
	}

	forms, err := store.Impl().RetrieveFormsByID(ctx, batch)
	if err != nil {
		slog.WarnContext(ctx, "Failed to batch-retrieve forms for report", common.ErrAttr(err))
		return
	}

	formMap := make(map[int32]*dbgen.Form, len(forms))
	for _, form := range forms {
		formMap[form.ID] = form
	}

	topForms := make([]*email.FormStat, 0, len(formsStats))
	for _, fs := range formsStats {
		form, ok := formMap[fs.FormID]
		if !ok {
			slog.DebugContext(ctx, "Skipping unknown form in report", "formID", fs.FormID)
			continue
		}

		percent := float64(fs.CurrentSubmissions) / float64(report.TotalFormSubmissions) * 100
		change := percentChange(fs.CurrentSubmissions, fs.PrevSubmissions)

		topForms = append(topForms, &email.FormStat{
			Name:      form.Name,
			URL:       form.URL,
			Link:      formDashboardURL(ctx, portalURL, hasher, form),
			Count:     fs.CurrentSubmissions,
			Percent:   percent,
			Change:    change,
			Alternate: len(topForms)%2 == 1,
		})
	}

	report.TopForms = topForms
}

func fillPropertyReportDetails(ctx context.Context, store db.Implementor, report *email.UsageReportContext, stats *common.UserReportStats, portalURL string, hasher common.IdentifierHasher) {
	if stats == nil || ((len(stats.Properties) == 0 || report.TotalRequests == 0) && len(stats.SecurityEvents) == 0) {
		return
	}

	props := stats.Properties
	if len(props) > topPropertiesLimit {
		props = props[:topPropertiesLimit]
	}

	batch := make(map[int32]uint, len(props)+len(stats.SecurityEvents))
	if report.TotalRequests > 0 {
		for _, ps := range props {
			batch[ps.PropertyID] = 0
		}
	}
	for _, candidate := range stats.SecurityEvents {
		batch[candidate.PropertyID] = 0
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

	if report.TotalRequests > 0 {
		topProperties := make([]*email.PropertyStat, 0, len(props))
		for _, ps := range props {
			prop, ok := propMap[ps.PropertyID]
			if !ok {
				slog.DebugContext(ctx, "Skipping unknown property in report", "propID", ps.PropertyID)
				continue
			}

			topProperties = append(topProperties, &email.PropertyStat{
				Name:      prop.Name,
				Domain:    common.DisplayPropertyDomain(prop.Domain, prop.AllowSubdomains),
				Link:      propertyDashboardURL(ctx, portalURL, hasher, prop) + "?" + report.UTM,
				Count:     ps.CurrentRequests,
				Percent:   float64(ps.CurrentRequests) / float64(report.TotalRequests) * 100,
				Change:    percentChange(ps.CurrentRequests, ps.PrevRequests),
				Alternate: len(topProperties)%2 == 1,
			})
		}
		report.TopProperties = topProperties
	}

	highlights := make([]*email.SecurityEventStat, 0, len(stats.SecurityEvents))
	for _, candidate := range stats.SecurityEvents {
		prop, ok := propMap[candidate.PropertyID]
		if !ok {
			slog.DebugContext(ctx, "Skipping unknown property protection candidate", "propID", candidate.PropertyID)
			continue
		}
		failedVerifies := uint64(0)
		if candidate.FailureQualified {
			failedVerifies = candidate.FailedVerifies
		}
		highlights = append(highlights, &email.SecurityEventStat{
			Name:           prop.Name,
			Link:           propertyDashboardURL(ctx, portalURL, hasher, prop) + "?" + report.UTM,
			Date:           candidate.Timestamp.UTC().Format(time.DateOnly),
			Requests:       candidate.Requests,
			Verifies:       candidate.Verifies,
			FailedVerifies: failedVerifies,
			Alternate:      len(highlights)%2 == 1,
		})
	}
	report.SecurityEvents = highlights
}

func propertyDashboardURL(ctx context.Context, portalURL string, hasher common.IdentifierHasher, property *dbgen.Property) string {
	if (len(portalURL) == 0) || (hasher == nil) || (property == nil) || (!property.OrgID.Valid) {
		return ""
	}

	link, err := url.JoinPath(portalURL,
		common.OrgEndpoint,
		hasher.Encrypt(int(property.OrgID.Int32)),
		common.PropertyEndpoint,
		hasher.Encrypt(int(property.ID)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to build property dashboard URL", "propID", property.ID, common.ErrAttr(err))
		return ""
	}

	return link
}
