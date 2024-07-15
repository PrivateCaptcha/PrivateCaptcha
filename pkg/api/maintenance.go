package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

func (s *server) checkUsageLimits(ctx context.Context, interval, initialPause time.Duration, maxUsers int) {
	time.Sleep(initialPause)

	slog.DebugContext(ctx, "Checking usage limits", "interval", interval.String(), "maxUsers", maxUsers)
	const usageLimitsLock = "usage_limits_job"

	ticker := time.NewTicker(interval)
	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
			ticker.Stop()
		case <-ticker.C:
			tnow := time.Now().UTC()
			// we use a smaller interval for the actual lock duration to account for monotonous clock discrepancies
			if _, err := s.businessDB.AcquireLock(ctx, usageLimitsLock, nil /*data*/, tnow.Add(interval-1*time.Second)); err == nil {
				if _, err := db.CheckUsageLimits(ctx, s.businessDB, s.timeSeries, tnow.Add(-interval), maxUsers); err != nil {
					// NOTE: in usual circumstances we do not release the lock, letting it expire by TTL, thus effectively
					// preventing other possible maintenance jobs during the interval. The only use-case is when the job
					// itself fails, then we want somebody to retry "sooner"
					s.businessDB.ReleaseLock(ctx, usageLimitsLock)
				}
			} else {
				level := slog.LevelError
				if err == db.ErrLocked {
					level = slog.LevelWarn
				}
				slog.Log(ctx, level, "Failed to acquire a lock for checking limits", common.ErrAttr(err))
			}
		}
	}

	slog.DebugContext(ctx, "Finished checking usage limits")
}
