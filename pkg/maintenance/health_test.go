package maintenance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveEndpoint(t *testing.T) {
	healthCheck := &HealthCheckJob{}

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	w1 := httptest.NewRecorder()
	healthCheck.LiveHandler(w1, req)

	if w1.Code != http.StatusOK {
		t.Errorf("Unexpected status code %d", w1.Code)
	}

	healthCheck.Shutdown(context.TODO())

	w2 := httptest.NewRecorder()
	healthCheck.LiveHandler(w2, req)

	if w2.Code != http.StatusInternalServerError {
		t.Errorf("Unexpected status code %d", w2.Code)
	}
}
