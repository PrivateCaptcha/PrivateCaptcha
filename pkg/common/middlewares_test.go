package common

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/justinas/alice"
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
		Prefix: "/",
	}

	handlerIDs := make([]string, 0, len(testCases))
	handlerFunc := func(handlerIDFunc func() string) func(http.Handler) http.Handler {
		return func(h http.Handler) http.Handler {
			handlerIDs = append(handlerIDs, handlerIDFunc())
			return h
		}
	}

	devNullHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for _, tc := range testCases {
		rg.Handle(rg.Route("any", tc.parts...), alice.New(handlerFunc(rg.LastPath)), devNullHandler)
	}

	rg.Register(&http.ServeMux{})

	if len(handlerIDs) != len(testCases) {
		t.Fatal("No handler registered")
	}

	for i, hid := range handlerIDs {
		if tc := testCases[i]; hid != tc.expected {
			t.Errorf("Actual path (%v) is different from expected (%v)", hid, tc.expected)
		}
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

func TestRedirectWithHtmxHeader(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderHtmxRequest, "true")
	w := httptest.NewRecorder()

	Redirect("/dashboard", http.StatusOK, w, req)

	// Check that HX-Redirect header is set
	if hxRedirect := w.Header().Get(headerHtmxRedirect); hxRedirect != "/dashboard" {
		t.Errorf("Expected HX-Redirect header to be '/dashboard', got %q", hxRedirect)
	}

	// Check the status code
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRedirectWithoutHtmxHeader(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	Redirect("/dashboard", http.StatusOK, w, req)

	// Check that Location header is set for regular redirect
	if location := w.Header().Get("Location"); location != "/dashboard" {
		t.Errorf("Expected Location header to be '/dashboard', got %q", location)
	}

	// Check the status code (http.Redirect uses StatusSeeOther)
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status code %d, got %d", http.StatusSeeOther, w.Code)
	}
}

func TestIntPathArg(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		pathValue     string
		expectedID    int32
		expectedError bool
	}{
		{"valid_int", "123", 123, false},
		{"negative_int", "-456", -456, false},
		{"zero", "0", 0, false},
		{"empty_string", "", 0, true},
		{"invalid_string", "abc", 0, true},
		{"float_value", "3.14", 0, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.SetPathValue("id", tc.pathValue)

			id, value, err := IntPathArg(req, "id", nil)

			if tc.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if id != tc.expectedID {
				t.Errorf("Expected ID %d, got %d", tc.expectedID, id)
			}

			if value != tc.pathValue {
				t.Errorf("Expected value %q, got %q", tc.pathValue, value)
			}
		})
	}
}

func TestIntPathArgOverflow(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Use a value larger than int32 max
	req.SetPathValue("id", fmt.Sprintf("%d", int64(math.MaxInt32)+1))

	_, _, err := IntPathArg(req, "id", nil)

	// Should fail because the value is out of int32 range
	if err == nil {
		t.Error("Expected error for overflow value")
	}
}

func TestStrPathArg(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		pathValue     string
		expectedError bool
	}{
		{"valid_string", "abc123", false},
		{"with_dash", "test-value", false},
		{"empty_string", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.SetPathValue("name", tc.pathValue)

			value, err := StrPathArg(req, "name")

			if tc.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if value != tc.pathValue {
				t.Errorf("Expected value %q, got %q", tc.pathValue, value)
			}
		})
	}
}

func TestXSRFMiddleware(t *testing.T) {
	t.Parallel()

	xsrf := &XSRFMiddleware{
		Key:     "test-secret-key",
		Timeout: 1 * time.Hour,
	}

	userID := "user123"

	// Generate token
	token := xsrf.Token(userID)
	if token == "" {
		t.Error("Expected non-empty token")
	}

	// Verify token
	if !xsrf.VerifyToken(token, userID) {
		t.Error("Token verification failed")
	}

	// Verify with wrong user ID
	if xsrf.VerifyToken(token, "wrong-user") {
		t.Error("Token should not verify with wrong user ID")
	}

	// Verify wrong token
	if xsrf.VerifyToken("invalid-token", userID) {
		t.Error("Invalid token should not verify")
	}
}

func TestGenerateETag(t *testing.T) {
	t.Parallel()

	// Test with single part
	etag1 := GenerateETag("part1")
	if etag1 == "" {
		t.Error("Expected non-empty ETag")
	}

	// Test with multiple parts
	etag2 := GenerateETag("part1", "part2", "part3")
	if etag2 == "" {
		t.Error("Expected non-empty ETag")
	}

	// Different parts should produce different ETags
	if etag1 == etag2 {
		t.Error("Different inputs should produce different ETags")
	}

	// Same parts should produce same ETags
	etag3 := GenerateETag("part1")
	if etag1 != etag3 {
		t.Error("Same inputs should produce same ETags")
	}
}

func TestRouteGeneratorHandle(t *testing.T) {
	t.Parallel()

	rg := &RouteGenerator{Prefix: "/api/"}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	emptyChain := alice.New()

	// Add a route
	r := rg.Route("any", "api", "test")
	rg.Handle(r, emptyChain, handler)

	// Verify route was added
	route, found := rg.Handler(r)
	if !found {
		t.Error("Expected route to be found after Handle")
	}
	if route == nil {
		t.Error("Expected route to be non-nil")
	}
	if route.Prefix != r.Prefix {
		t.Errorf("Expected prefix %q, got %q", r.Prefix, route.Prefix)
	}
	if route.Path != r.Path {
		t.Errorf("Expected path %q, got %q", r.Path, route.Path)
	}
}

func TestRouteGeneratorHandleUpdate(t *testing.T) {
	t.Parallel()

	rg := &RouteGenerator{Prefix: "/api/"}

	handler1 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	emptyChain := alice.New()
	r := rg.Route("any", "api", "test")

	// Add a route
	rg.Handle(r, emptyChain, handler1)

	// Update the same route
	rg.Handle(r, emptyChain, handler2)

	// Verify route was updated (should only have one route, not two)
	count := 0
	for _, route := range rg.routes {
		if (route.Prefix == r.Prefix) && (route.Path == r.Path) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 route with pattern, got %d", count)
	}
}

func TestRouteGeneratorHandlerNotFound(t *testing.T) {
	t.Parallel()

	rg := &RouteGenerator{Prefix: "/api/"}

	// Try to find a route that doesn't exist
	route, found := rg.Handler(&Route{Prefix: "GET /", Path: "api/nonexistent"})
	if found {
		t.Error("Expected route not to be found")
	}
	if route != nil {
		t.Error("Expected nil route when not found")
	}
}

func TestRouteGeneratorMultipleRoutes(t *testing.T) {
	t.Parallel()

	rg := &RouteGenerator{Prefix: "/api/"}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	emptyChain := alice.New()

	patterns := []*Route{
		rg.Route("GET", "api", "users"),
		rg.Route("POST", "api", "users"),
		rg.Route("GET", "api", "orgs"),
	}

	for _, p := range patterns {
		rg.Handle(p, emptyChain, handler)
	}

	// Verify all routes were added
	for _, p := range patterns {
		route, found := rg.Handler(p)
		if !found {
			t.Errorf("Expected route %q to be found", p)
		}
		if route == nil {
			t.Errorf("Expected non-nil route for %q", p)
		}
	}

	// Verify total count
	if len(rg.routes) != len(patterns) {
		t.Errorf("Expected %d routes, got %d", len(patterns), len(rg.routes))
	}
}
