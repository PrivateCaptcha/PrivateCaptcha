package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func (s *server) checkUsageLimits(ctx context.Context, interval, initialPause time.Duration, maxUsers int) {
	time.Sleep(initialPause)

	slog.DebugContext(ctx, "Checking usage limits", "interval", interval.String(), "maxUsers", maxUsers)

	ticker := time.NewTicker(interval)
	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
			ticker.Stop()
		case <-ticker.C:
			_, _ = s.doCheckUsageLimits(ctx, time.Now().Add(-interval), maxUsers)
		}
	}

	slog.DebugContext(ctx, "Finished checking usage limits")
}

func (s *server) doCheckUsageLimits(ctx context.Context, from time.Time, maxUsers int) ([]*common.UserTimeCount, error) {
	violations, err := s.timeSeries.FindUserLimitsViolations(ctx, from, maxUsers)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to check user limits", common.ErrAttr(err))
		return nil, err
	}

	if len(violations) == 0 {
		slog.InfoContext(ctx, "No violations found")
		return violations, nil
	}

	err = s.businessDB.AddUsageLimitsViolations(ctx, violations)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to add usage limits violations", common.ErrAttr(err))
		return violations, err
	}

	return violations, nil
}
