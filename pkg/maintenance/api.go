package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type UsageLimitsJob struct {
	MaxUsers   int
	From       time.Time
	BusinessDB *db.BusinessStore
	TimeSeries *db.TimeSeriesStore
}

var _ common.PeriodicJob = (*UsageLimitsJob)(nil)

func (j *UsageLimitsJob) Interval() time.Duration {
	return 30 * time.Minute
}

func (j *UsageLimitsJob) Jitter() time.Duration {
	return 5 * time.Minute
}

func (j *UsageLimitsJob) Name() string {
	return "usage_limits_job"
}

func (j *UsageLimitsJob) RunOnce(ctx context.Context) error {
	// NOTE: because of {maxUsers} limit, we _may_ not find all violations, but it's considered OK
	// at this time business-wise, as they will be dealt with likely semi-manually anyways
	violations, err := j.TimeSeries.FindUserLimitsViolations(ctx, j.From, j.MaxUsers)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to check user limits", common.ErrAttr(err))
		return err
	}
	// NOTE: this will lead to possible "missed" intervals if we actually failed to add the limits below
	// but it's OK since this is not a deterministic process anyways due to many factors (e.g. maxUsers limit)
	j.From = time.Now().UTC()

	if len(violations) == 0 {
		slog.InfoContext(ctx, "No violations found")
		return nil
	}

	err = j.BusinessDB.AddUsageLimitsViolations(ctx, violations)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to add usage limits violations", common.ErrAttr(err))
	}

	return err
}

type NotifyLimitsViolationsJob struct {
	Mailer common.AdminMailer
	Store  *db.BusinessStore
}

var _ common.PeriodicJob = (*NotifyLimitsViolationsJob)(nil)

func (j *NotifyLimitsViolationsJob) Interval() time.Duration {
	return 1 * time.Hour
}

func (j *NotifyLimitsViolationsJob) Jitter() time.Duration {
	return 5 * time.Minute
}

func (j *NotifyLimitsViolationsJob) Name() string {
	return "notify_limits_violations_job"
}

func (j *NotifyLimitsViolationsJob) RunOnce(ctx context.Context) error {
	consecutiveViolations, err := j.Store.RetrieveUsersWithConsecutiveViolations(ctx)
	if err != nil {
		return err
	}

	emails := make([]string, 0, len(consecutiveViolations))
	for _, v := range consecutiveViolations {
		emails = append(emails, v.User.Email)
	}

	// rate of volume change between plans is approximately 100% and we check if user is approaching it
	const rate = 1.75
	// we always grab everybody since the start of the month
	from := common.StartOfMonth()

	largeViolations, err := j.Store.RetrieveUsersWithLargeViolations(ctx, from, rate)
	if err != nil {
		return err
	}

	for _, v := range largeViolations {
		// NOTE: it's OK to duplicate emails _at this point_ as processing is manual anyways
		emails = append(emails, v.User.Email)
	}

	if len(emails) == 0 {
		slog.DebugContext(ctx, "No usage violations found")
		return nil
	}

	return j.Mailer.SendUsageViolations(ctx, emails)
}

type ThrottleViolationsJob struct {
	Stage      string
	UserLimits common.Cache[int32, *common.UserLimitStatus]
	Store      *db.BusinessStore
	From       time.Time
}

var _ common.PeriodicJob = (*ThrottleViolationsJob)(nil)

func (j *ThrottleViolationsJob) Interval() time.Duration {
	return 2 * time.Hour
}

func (j *ThrottleViolationsJob) Jitter() time.Duration {
	return 10 * time.Minute
}

func (j *ThrottleViolationsJob) Name() string {
	return "throttle_violations_job"
}

func (j *ThrottleViolationsJob) RunOnce(ctx context.Context) error {
	// rate of volume change between plans is approximately 100%, but we also double check below
	const rate = 2.0
	violations, err := j.Store.RetrieveUsersWithLargeViolations(ctx, j.From, rate)
	if err != nil {
		return err
	}
	j.From = time.Now().UTC()

	for _, v := range violations {
		productID := v.UsageLimitViolation.PaddleProductID
		if len(productID) == 0 {
			continue
		}

		plan, err := billing.FindPlanByProductID(productID, j.Stage)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to find billing plan", "productID", productID)
			continue
		}

		if plan.RequestsLimit > int64(v.UsageLimitViolation.RequestsLimit) {
			slog.ErrorContext(ctx, "Skipping stale violation limit from ClickHouse", "userID", v.User.ID,
				"productID", productID, "newLimit", plan.RequestsLimit, "oldLimit", v.UsageLimitViolation.RequestsLimit)
			continue
		}

		if plan.ShouldBeThrottled(int64(v.UsageLimitViolation.RequestsCount)) {
			slog.InfoContext(ctx, "Found user to be throttled", "userID", v.User.ID, "productID", productID,
				"count", v.UsageLimitViolation.RequestsCount, "limit", v.UsageLimitViolation.RequestsLimit)

			status := &common.UserLimitStatus{Status: v.Status, Limit: v.UsageLimitViolation.RequestsLimit}
			if err := j.UserLimits.Set(ctx, int32(v.User.ID), status, db.UserLimitTTL); err != nil {
				slog.ErrorContext(ctx, "Failed to add user to block", "userID", v.User.ID, common.ErrAttr(err))
			}
		}
	}

	return nil
}
