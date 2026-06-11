package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/jpillora/backoff"
)

type HealthCheckJob struct {
	BusinessDB       db.Implementor
	TimeSeriesDB     common.TimeSeriesStore
	postgresFlag     atomic.Int32
	clickhouseFlag   atomic.Int32
	shuttingDownFlag atomic.Int32
	CheckInterval    common.ConfigItem
	MaintenanceMode  common.ConfigItem
	Metrics          common.PlatformMetrics
	StrictReadiness  bool
}

const (
	greenPage  = `<!DOCTYPE html><html><body style="background-color: green;"></body></html>`
	orangePage = `<!DOCTYPE html><html><body style="background-color: orange;"></body></html>`
	redPage    = `<!DOCTYPE html><html><body style="background-color: red;"></body></html>`
	FlagTrue   = 1
	FlagFalse  = 0
)

var _ common.PeriodicJob = (*HealthCheckJob)(nil)

func (j *HealthCheckJob) Interval() time.Duration {
	return time.Duration(max(1, config.AsInt(j.CheckInterval, 60))) * time.Second
}

func (j *HealthCheckJob) Timeout() time.Duration {
	return 10 * time.Second
}

func (j *HealthCheckJob) Jitter() time.Duration {
	return 1
}

func (j *HealthCheckJob) Name() string {
	return "health_check_job"
}

func (j *HealthCheckJob) NewParams() any {
	return struct{}{}
}

func (j *HealthCheckJob) Trigger() <-chan struct{} {
	return nil
}

func (hc *HealthCheckJob) RunOnce(ctx context.Context, params any) error {
	pgStatus := hc.checkPostgres(ctx)
	hc.postgresFlag.Store(pgStatus)

	chStatus := hc.checkClickHouse(ctx)
	hc.clickhouseFlag.Store(chStatus)

	hc.Metrics.ObserveHealth((pgStatus == FlagTrue), (chStatus == FlagTrue))
	hc.Metrics.ObserveCacheHitRatio(hc.BusinessDB.CacheHitRatio())

	return nil
}

func (hc *HealthCheckJob) checkClickHouse(ctx context.Context) int32 {
	b := &backoff.Backoff{
		Min:    100 * time.Millisecond,
		Max:    400 * time.Millisecond,
		Factor: 1.5,
		Jitter: true,
	}

	const maxAttempts = 3
	var err error

	for i := 0; i < maxAttempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "ClickHouse ping context cancelled", common.ErrAttr(ctx.Err()))
				return int32(FlagFalse)
			case <-time.After(b.Duration()):
			}
		}

		if err = hc.TimeSeriesDB.Ping(ctx); err == nil {
			return int32(FlagTrue)
		} else {
			slog.WarnContext(ctx, "ClickHouse ping attempt failed", "attempt", i+1, common.ErrAttr(err))
		}
	}

	slog.ErrorContext(ctx, "Failed to ping ClickHouse", "attempts", maxAttempts, common.ErrAttr(err))

	return int32(FlagFalse)
}

func (hc *HealthCheckJob) checkPostgres(ctx context.Context) int32 {
	b := &backoff.Backoff{
		Min:    100 * time.Millisecond,
		Max:    400 * time.Millisecond,
		Factor: 1.5,
		Jitter: true,
	}

	const maxAttempts = 3
	var err error

	for i := 0; i < maxAttempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "Postgres ping context cancelled", common.ErrAttr(ctx.Err()))
				return int32(FlagFalse)
			case <-time.After(b.Duration()):
			}
		}

		if err = hc.BusinessDB.Ping(ctx); err == nil {
			return int32(FlagTrue)
		} else {
			slog.WarnContext(ctx, "Postgres ping attempt failed", "attempt", i+1, common.ErrAttr(err))
		}
	}

	slog.ErrorContext(ctx, "Failed to ping Postgres", "attempts", maxAttempts, common.ErrAttr(err))

	return int32(FlagFalse)
}

func (hc *HealthCheckJob) IsPostgresHealthy() bool {
	return hc.postgresFlag.Load() == FlagTrue
}

func (hc *HealthCheckJob) IsClickHouseHealthy() bool {
	return hc.clickhouseFlag.Load() == FlagTrue
}

func (hc *HealthCheckJob) IsShuttingDown() bool {
	return hc.shuttingDownFlag.Load() == FlagTrue
}

func (hc *HealthCheckJob) Shutdown(ctx context.Context) {
	slog.DebugContext(ctx, "Shutting down health check job")
	hc.shuttingDownFlag.Store(FlagTrue)
}

func (hc *HealthCheckJob) LiveHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (hc *HealthCheckJob) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(common.HeaderContentType, common.ContentTypeHTML)

	shuttingDown := hc.IsShuttingDown()
	healthy := hc.IsPostgresHealthy() && hc.IsClickHouseHealthy()
	maintenanceMode := config.AsBool(hc.MaintenanceMode)

	if !shuttingDown && (healthy || !hc.StrictReadiness || maintenanceMode) {
		w.WriteHeader(http.StatusOK)
		if healthy {
			fmt.Fprintln(w, greenPage)
		} else {
			fmt.Fprintln(w, orangePage)
		}
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, redPage)
	}
}
