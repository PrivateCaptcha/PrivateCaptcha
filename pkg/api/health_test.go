package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func TestHealthEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	healthCheck := &maintenance.HealthCheckJob{
		BusinessDB:   store,
		TimeSeriesDB: timeSeries,
		Router:       nil,
	}

	if err := healthCheck.RunOnce(context.TODO()); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	healthCheck.HandlerFunc(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Unexpected status code %d", w.Code)
	}
}
