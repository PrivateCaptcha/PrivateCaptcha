package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
)

func TestReadyEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	healthCheck := &maintenance.HealthCheckJob{
		BusinessDB:   store,
		TimeSeriesDB: timeSeries,
		Metrics:      monitoring.NewStub(),
	}

	if err := healthCheck.RunOnce(t.Context(), healthCheck.NewParams()); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	healthCheck.ReadyHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Unexpected status code %d", w.Code)
	}
}
