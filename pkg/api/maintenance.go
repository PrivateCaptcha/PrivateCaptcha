package api

import (
	"context"
	"log/slog"
	"time"

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
	_, err := j.findViolations(ctx)
	// NOTE: this will lead to possible "missed" intervals if we actually failed to check the limits
	// but it's OK since this is not a deterministic process anyways due to many factors (e.g. maxUsers limit)
	j.From = time.Now().UTC()

	return err
}

func (j *UsageLimitsJob) findViolations(ctx context.Context) ([]*common.UserTimeCount, error) {
	violations, err := j.TimeSeries.FindUserLimitsViolations(ctx, j.From, j.MaxUsers)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to check user limits", common.ErrAttr(err))
		return nil, err
	}

	if len(violations) == 0 {
		slog.InfoContext(ctx, "No violations found")
		return violations, nil
	}

	err = j.BusinessDB.AddUsageLimitsViolations(ctx, violations)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to add usage limits violations", common.ErrAttr(err))
		return violations, err
	}

	return violations, nil
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

	const rate = 1.25
	largeViolations, err := j.Store.RetrieveUsersWithLargeViolations(ctx, rate)
	if err != nil {
		return err
	}

	for _, v := range largeViolations {
		// NOTE: it's OK to duplicate emails _at this point_ as processing is manual anyways
		emails = append(emails, v.User.Email)
	}

	return j.Mailer.SendUsageViolations(ctx, emails)
}
