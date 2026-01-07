package common

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouteGenerator(t *testing.T) {
	testCases := []struct {
		parts    []string
		expected string
	}{
		{[]string{"login"}, "login"},
		{[]string{"org", "new"}, "org/new"},
		{[]string{"org", "1", "property", "1"}, "org/1/property/1"},
		{[]string{"org", "{org}", "property", "{prop}"}, "org/{org}/property/{prop}"},
	}

	rg := &RouteGenerator{
		Prefix: "privatecaptcha.com/",
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("route_path_%v", i), func(t *testing.T) {
			rg.Route("any", tc.parts...)

			if actual := rg.LastPath(); actual != tc.expected {
				t.Errorf("Actual path (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

func TestRecoveredMiddleware(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	recovered := Recovered(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	recovered.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestRecoveredMiddlewareNoPanic(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	recovered := Recovered(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	recovered.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestCatchAll(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		expectedCode int
	}{
		{"root path", "/", http.StatusOK},
		{"non-root path", "/notfound", http.StatusNotFound},
		{"nested path", "/nested/path", http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()

			CatchAll(w, req)

			if w.Code != tc.expectedCode {
				t.Errorf("Expected status code %d, got %d", tc.expectedCode, w.Code)
			}
		})
	}
}

func TestNoopMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := NoopMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestServiceMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc, ok := r.Context().Value(ServiceContextKey).(string)
		if !ok || svc != "test-service" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	wrapped := ServiceMiddleware("test-service")(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}
