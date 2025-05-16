package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type HealthCheckJob struct {
	BusinessDB       db.Implementor
	TimeSeriesDB     common.TimeSeriesStore
	Router           *http.ServeMux
	postgresFlag     atomic.Int32
	clickhouseFlag   atomic.Int32
	shuttingDownFlag atomic.Int32
	CheckInterval    common.ConfigItem
}

const (
	greenPage = `<!DOCTYPE html><html><body style="background-color: green;"></body></html>`
	redPage   = `<!DOCTYPE html><html><body style="background-color: red;"></body></html>`
	FlagTrue  = 1
	FlagFalse = 0
)

var _ common.PeriodicJob = (*HealthCheckJob)(nil)

func (j *HealthCheckJob) Interval() time.Duration {
	intervalType := j.CheckInterval.Value()
	if intervalType == "slow" {
		return 1 * time.Minute
	}

	return 5 * time.Second
}

func (j *HealthCheckJob) Jitter() time.Duration {
	return 1
}

func (j *HealthCheckJob) Name() string {
	return "health_check"
}

func (hc *HealthCheckJob) RunOnce(ctx context.Context) error {
	hc.postgresFlag.Store(hc.checkPostgres(ctx))
	hc.clickhouseFlag.Store(hc.checkClickHouse(ctx))

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

func (hc *HealthCheckJob) HandlerFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(common.HeaderContentType, common.ContentTypeHTML)
	healthy := hc.isPostgresHealthy() && hc.isClickHouseHealthy()
	shuttingDown := hc.isShuttingDown()
	if healthy && !shuttingDown {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, greenPage)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, redPage)
	}
}
