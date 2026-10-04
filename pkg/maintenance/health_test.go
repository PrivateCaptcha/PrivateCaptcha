package maintenance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
)

type stubTimeSeries struct {
	*db.MemoryTimeSeries
	pings int32
	err   error
}

func (s *stubTimeSeries) Ping(ctx context.Context) error {
	atomic.AddInt32(&s.pings, 1)
	return s.err
}

func TestLiveEndpoint(t *testing.T) {
	healthCheck := &HealthCheckJob{}

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	healthCheck.LiveHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Unexpected status code %d", w.Code)
	}
}

func TestCheckClickHouseNotConfigured(t *testing.T) {
	ts := &stubTimeSeries{
		MemoryTimeSeries: db.NewMemoryTimeSeries(),
		err:              db.ErrClickHouseNotConfigured,
	}
	hc := &HealthCheckJob{
		TimeSeriesDB: ts,
		Metrics:      monitoring.NewStub(),
	}

	start := time.Now()
	status := hc.checkClickHouse(context.Background())
	elapsed := time.Since(start)

	if status != int32(FlagFalse) {
		t.Errorf("expected FlagFalse (%d), got %d", FlagFalse, status)
	}
	if pings := atomic.LoadInt32(&ts.pings); pings != 1 {
		t.Errorf("expected exactly one Ping call (no retries), got %d", pings)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected no backoff delay for not-configured state, took %v", elapsed)
	}
}

func TestCheckClickHouseHealthy(t *testing.T) {
	ts := &stubTimeSeries{
		MemoryTimeSeries: db.NewMemoryTimeSeries(),
		err:              nil,
	}
	hc := &HealthCheckJob{
		TimeSeriesDB: ts,
		Metrics:      monitoring.NewStub(),
	}

	status := hc.checkClickHouse(context.Background())

	if status != int32(FlagTrue) {
		t.Errorf("expected FlagTrue (%d), got %d", FlagTrue, status)
	}
	if pings := atomic.LoadInt32(&ts.pings); pings != 1 {
		t.Errorf("expected exactly one Ping call, got %d", pings)
	}
}

func TestCheckClickHouseRetriesOnTransientError(t *testing.T) {
	ts := &stubTimeSeries{
		MemoryTimeSeries: db.NewMemoryTimeSeries(),
		err:              errors.New("connection refused"),
	}
	hc := &HealthCheckJob{
		TimeSeriesDB: ts,
		Metrics:      monitoring.NewStub(),
	}

	status := hc.checkClickHouse(context.Background())

	if status != int32(FlagFalse) {
		t.Errorf("expected FlagFalse (%d) after retries, got %d", FlagFalse, status)
	}
	if pings := atomic.LoadInt32(&ts.pings); pings != 3 {
		t.Errorf("expected three Ping calls (with retries), got %d", pings)
	}
}
