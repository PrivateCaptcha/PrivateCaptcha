package maintenance

import (
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

	w := httptest.NewRecorder()
	healthCheck.LiveHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Unexpected status code %d", w.Code)
	}
}
