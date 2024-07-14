package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func CheckUsageLimits(ctx context.Context, businessDB *BusinessStore, timeSeries *TimeSeriesStore, from time.Time, maxUsers int) ([]*common.UserTimeCount, error) {
	violations, err := timeSeries.FindUserLimitsViolations(ctx, from, maxUsers)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to check user limits", common.ErrAttr(err))
		return nil, err
	}

	if len(violations) == 0 {
		slog.InfoContext(ctx, "No violations found")
		return violations, nil
	}

	err = businessDB.AddUsageLimitsViolations(ctx, violations)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to add usage limits violations", common.ErrAttr(err))
		return violations, err
	}

	return violations, nil
}
