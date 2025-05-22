package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func TestReadyEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	healthCheck := &maintenance.HealthCheckJob{
		BusinessDB:   store,
		TimeSeriesDB: timeSeries,
	}

	if err := healthCheck.RunOnce(context.TODO()); err != nil {
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
