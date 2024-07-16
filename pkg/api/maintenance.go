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
	return 3 * time.Hour
}

func (j *UsageLimitsJob) Jitter() time.Duration {
	return j.Interval() / 2
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
