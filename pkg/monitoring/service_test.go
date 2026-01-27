package monitoring

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestNewService(t *testing.T) {
	t.Parallel()

	service := NewService()

	if service == nil {
		t.Fatal("Expected NewService to return non-nil service")
	}

	if service.Registry == nil {
		t.Error("Expected Registry to be non-nil")
	}

	if service.puzzleCounter == nil {
		t.Error("Expected puzzleCounter to be non-nil")
	}

	if service.verifyCounter == nil {
		t.Error("Expected verifyCounter to be non-nil")
	}

	if service.portalErrorCounter == nil {
		t.Error("Expected portalErrorCounter to be non-nil")
	}

	if service.apiErrorCounter == nil {
		t.Error("Expected apiErrorCounter to be non-nil")
	}

	if service.dropCounter == nil {
		t.Error("Expected dropCounter to be non-nil")
	}

	if service.hitRatioGauge == nil {
		t.Error("Expected hitRatioGauge to be non-nil")
	}

	if service.clickhouseHealthGauge == nil {
		t.Error("Expected clickhouseHealthGauge to be non-nil")
	}

	if service.postgresHealthGauge == nil {
		t.Error("Expected postgresHealthGauge to be non-nil")
	}
}

func TestServiceObservePuzzleCreated(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test that ObservePuzzleCreated does not panic
	service.ObservePuzzleCreated(123)
	service.ObservePuzzleCreated(456)
}

func TestServiceObservePuzzleVerified(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test various combinations of parameters
	service.ObservePuzzleVerified(123, "success", false)
	service.ObservePuzzleVerified(123, "failure", true)
	service.ObservePuzzleVerified(456, "error", false)
}

func TestServiceObserveHttpError(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test that ObserveHttpError does not panic
	service.ObserveHttpError("/login", "GET", 400)
	service.ObserveHttpError("/register", "POST", 500)
	service.ObserveHttpError("/settings", "PUT", 404)
}

func TestServiceObserveApiError(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test that ObserveApiError does not panic
	service.ObserveApiError("/api/puzzle", "GET", 401)
	service.ObserveApiError("/api/verify", "POST", 400)
}

func TestServiceObserveCacheHitRatio(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test various hit ratios
	service.ObserveCacheHitRatio(0.0)
	service.ObserveCacheHitRatio(0.5)
	service.ObserveCacheHitRatio(1.0)
}

func TestServiceObserveEventDropped(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test different event types
	service.ObserveEventDropped(common.PuzzleEventType)
	service.ObserveEventDropped(common.VerifyEventType)
	service.ObserveEventDropped(common.SessionEventType)
}

func TestServiceObserveHealth(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test different health combinations
	service.ObserveHealth(true, true)
	service.ObserveHealth(true, false)
	service.ObserveHealth(false, true)
	service.ObserveHealth(false, false)
}

func TestServiceHandler(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Create a simple handler
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with metrics
	wrapped := service.Handler(inner)
	if wrapped == nil {
		t.Fatal("Expected Handler to return non-nil handler")
	}

	// Test that the wrapped handler works
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

func TestServiceCDNHandler(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Create a simple handler
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with CDN metrics
	wrapped := service.CDNHandler(inner)
	if wrapped == nil {
		t.Fatal("Expected CDNHandler to return non-nil handler")
	}

	// Test that the wrapped handler works
	req := httptest.NewRequest("GET", "/cdn/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

func TestServiceIgnoredHandler(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Create a simple handler
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with ignored metrics
	wrapped := service.IgnoredHandler(inner)
	if wrapped == nil {
		t.Fatal("Expected IgnoredHandler to return non-nil handler")
	}

	// Test that the wrapped handler works
	req := httptest.NewRequest("GET", "/ignored", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

func TestServiceHandlerIDFunc(t *testing.T) {
	t.Parallel()

	service := NewService()

	handlerIDFunc := func() string {
		return "/custom/handler"
	}

	middleware := service.HandlerIDFunc(handlerIDFunc)
	if middleware == nil {
		t.Fatal("Expected HandlerIDFunc to return non-nil middleware")
	}

	// Create a simple handler
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	wrapped := middleware(inner)
	if wrapped == nil {
		t.Fatal("Expected middleware to return non-nil handler")
	}

	// Test that the wrapped handler works
	req := httptest.NewRequest("GET", "/custom/handler", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

func TestLogged(t *testing.T) {
	t.Parallel()

	// Create a simple handler
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with logging
	wrapped := Logged(inner)
	if wrapped == nil {
		t.Fatal("Expected Logged to return non-nil handler")
	}

	// Test that the wrapped handler works
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}
}

func TestTraced(t *testing.T) {
	t.Parallel()

	// Create a simple handler
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with tracing
	wrapped := Traced(inner)
	if wrapped == nil {
		t.Fatal("Expected Traced to return non-nil handler")
	}

	// Test that the wrapped handler works
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", w.Code)
	}

	// Check that trace ID header was set
	traceID := w.Header().Get(common.HeaderTraceID)
	if traceID == "" {
		t.Error("Expected trace ID header to be set")
	}
}
