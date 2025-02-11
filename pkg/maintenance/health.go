package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/coreos/go-systemd/daemon"
)

type HealthCheckJob struct {
	BusinessDB       *db.BusinessStore
	TimeSeriesDB     *db.TimeSeriesStore
	Router           *http.ServeMux
	postgresFlag     atomic.Int32
	clickhouseFlag   atomic.Int32
	shuttingDownFlag atomic.Int32
	CheckInterval    common.ConfigItem
	WithSystemd      bool
}

const (
	greenPage = `<!DOCTYPE html><html><body style="background-color: green;"></body></html>`
	redPage   = `<!DOCTYPE html><html><body style="background-color: red;"></body></html>`
	flagTrue  = 1
	flagFalse = 0
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

	if hc.WithSystemd {
		if result := hc.checkHTTP(ctx); result == flagTrue {
			_, _ = daemon.SdNotify(false, daemon.SdNotifyWatchdog)
		}
	}

	return nil
}

func (hc *HealthCheckJob) checkClickHouse(ctx context.Context) int32 {
	result := int32(flagFalse)
	if err := hc.TimeSeriesDB.Ping(ctx); err == nil {
		result = flagTrue
	} else {
		slog.ErrorContext(ctx, "Failed to ping ClickHouse", common.ErrAttr(err))
	}
	return result
}

func (hc *HealthCheckJob) checkPostgres(ctx context.Context) int32 {
	result := int32(flagFalse)
	if err := hc.BusinessDB.Ping(ctx); err == nil {
		result = flagTrue
	} else {
		slog.ErrorContext(ctx, "Failed to ping Postgres", common.ErrAttr(err))
	}
	return result
}

func (hc *HealthCheckJob) isPostgresHealthy() bool {
	return hc.postgresFlag.Load() == flagTrue
}

func (hc *HealthCheckJob) isClickHouseHealthy() bool {
	return hc.clickhouseFlag.Load() == flagTrue
}

func (hc *HealthCheckJob) isShuttingDown() bool {
	return hc.shuttingDownFlag.Load() == flagTrue
}

func (hc *HealthCheckJob) checkHTTP(ctx context.Context) int32 {
	result := int32(flagFalse)
	req, err := http.NewRequest(http.MethodGet, "/"+common.HealthEndpoint, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to ping own health endpoint", common.ErrAttr(err))
		return result
	}
	w := httptest.NewRecorder()
	hc.Router.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode == http.StatusOK {
		result = flagTrue
	}
	return result
}

func (hc *HealthCheckJob) Shutdown(ctx context.Context) {
	slog.DebugContext(ctx, "Shutting down health check job")
	hc.shuttingDownFlag.Store(flagTrue)
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
