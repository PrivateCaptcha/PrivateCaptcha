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
)

type HealthCheckJob struct {
	BusinessDB       db.Implementor
	TimeSeriesDB     common.TimeSeriesStore
	postgresFlag     atomic.Int32
	clickhouseFlag   atomic.Int32
	shuttingDownFlag atomic.Int32
	CheckInterval    common.ConfigItem
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
	result := int32(FlagFalse)
	if err := hc.TimeSeriesDB.Ping(ctx); err == nil {
		result = FlagTrue
	} else {
		slog.ErrorContext(ctx, "Failed to ping ClickHouse", common.ErrAttr(err))
	}
	return result
}

func (hc *HealthCheckJob) checkPostgres(ctx context.Context) int32 {
	result := int32(FlagFalse)
	if err := hc.BusinessDB.Ping(ctx); err == nil {
		result = FlagTrue
	} else {
		slog.ErrorContext(ctx, "Failed to ping Postgres", common.ErrAttr(err))
	}
	return result
}

func (hc *HealthCheckJob) isPostgresHealthy() bool {
	return hc.postgresFlag.Load() == FlagTrue
}

func (hc *HealthCheckJob) isClickHouseHealthy() bool {
	return hc.clickhouseFlag.Load() == FlagTrue
}

func (hc *HealthCheckJob) isShuttingDown() bool {
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

	shuttingDown := hc.isShuttingDown()
	healthy := hc.isPostgresHealthy() && hc.isClickHouseHealthy()

	if !shuttingDown && (healthy || !hc.StrictReadiness) {
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
