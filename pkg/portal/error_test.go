package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestErrorPage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	testCases := []struct {
		code int
	}{
		{http.StatusForbidden},
		{http.StatusNotFound},
		{http.StatusUnauthorized},
		{http.StatusServiceUnavailable},
		{http.StatusInternalServerError},
	}

	for _, tc := range testCases {
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			req := httptest.NewRequest("GET", "/error/"+string(rune('0'+tc.code/100))+string(rune('0'+(tc.code/10)%10))+string(rune('0'+tc.code%10)), nil)
			req.SetPathValue(common.ParamCode, string(rune('0'+tc.code/100))+string(rune('0'+(tc.code/10)%10))+string(rune('0'+tc.code%10)))

			w := httptest.NewRecorder()
			server.error(w, req)

			if w.Code != tc.code {
				t.Errorf("Expected status code %d, got %d", tc.code, w.Code)
			}
		})
	}
}

func TestNotFoundHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	server.notFound(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestExpiredHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/expired", nil)
	w := httptest.NewRecorder()

	server.expired(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestPostClientSideError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	body := `{"error": "test error", "stack": "test stack trace"}`
	req := httptest.NewRequest("POST", "/error/report", strings.NewReader(body))
	req.Header.Set(common.HeaderContentType, common.ContentTypeJSON)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestErrorPageInvalidCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/error/99999", nil)
	req.SetPathValue(common.ParamCode, "99999")

	w := httptest.NewRecorder()
	server.error(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status code %d for invalid error code, got %d", http.StatusInternalServerError, w.Code)
	}
}
