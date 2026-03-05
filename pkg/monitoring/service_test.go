package monitoring

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestNewService(t *testing.T) {
	t.Parallel()

	service := NewService()

	if service == nil {
		t.Fatal("Expected NewService to return non-nil service")
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

func TestServiceObserveQueryDuration(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test various query durations
	service.ObserveQueryDuration(0.001)
	service.ObserveQueryDuration(0.05)
	service.ObserveQueryDuration(1.5)
}

func TestServiceObserveQueryError(t *testing.T) {
	t.Parallel()

	service := NewService()

	// Test that ObserveQueryError does not panic
	service.ObserveQueryError()
	service.ObserveQueryError()
}

type mockPoolStat struct {
	totalConns      int32
	acquireCount    int64
	acquireDuration time.Duration
}

func (m *mockPoolStat) TotalConns() int32              { return m.totalConns }
func (m *mockPoolStat) AcquireCount() int64            { return m.acquireCount }
func (m *mockPoolStat) AcquireDuration() time.Duration { return m.acquireDuration }

func TestServiceRegisterPgxPoolStats(t *testing.T) {
	t.Parallel()

	service := NewService()

	mock := &mockPoolStat{
		totalConns:      5,
		acquireCount:    100,
		acquireDuration: 2 * time.Second,
	}

	service.RegisterPgxPoolStats(func() PgxPoolStatProvider {
		return mock
	})

	// Gather metrics to verify the collector works
	metrics, err := service.Registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	found := map[string]bool{
		"server_pgxpool_total_conns":                    false,
		"server_pgxpool_acquire_count_total":            false,
		"server_pgxpool_acquire_duration_seconds_total": false,
	}

	for _, mf := range metrics {
		if _, ok := found[mf.GetName()]; ok {
			found[mf.GetName()] = true
		}
	}

	for name, wasFound := range found {
		if !wasFound {
			t.Errorf("Expected metric %q to be present", name)
		}
	}
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
