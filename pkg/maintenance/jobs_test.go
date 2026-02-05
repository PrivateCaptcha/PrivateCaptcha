package maintenance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
)

type stubOneOffJob struct {
	executed int32
}

func (j *stubOneOffJob) Name() string {
	return "stubOneOffJob"
}

func (j *stubOneOffJob) InitialPause() time.Duration {
	return 0
}

func (j *stubOneOffJob) NewParams() any {
	return struct{}{}
}

func (j *stubOneOffJob) RunOnce(ctx context.Context, params any) error {
	atomic.StoreInt32(&j.executed, 1)
	return nil
}

func (j *stubOneOffJob) wasExecuted() bool {
	return atomic.LoadInt32(&j.executed) == 1
}

type stubPeriodicJob struct {
	interval time.Duration
	timeout  time.Duration
	jitter   time.Duration
	executed int32
}

var _ common.PeriodicJob = (*stubPeriodicJob)(nil)

func (j *stubPeriodicJob) Name() string {
	return "stubPeriodicJob"
}

func (j *stubPeriodicJob) Trigger() <-chan struct{} {
	return nil
}

func (j *stubPeriodicJob) Interval() time.Duration {
	return j.interval
}

func (j *stubPeriodicJob) Timeout() time.Duration {
	return j.timeout
}

func (j *stubPeriodicJob) Jitter() time.Duration {
	return 1
}

func (j *stubPeriodicJob) NewParams() any {
	return struct{}{}
}

func (j *stubPeriodicJob) RunOnce(ctx context.Context, params any) error {
	atomic.StoreInt32(&j.executed, 1)
	return nil
}

func (j *stubPeriodicJob) wasExecuted() bool {
	return atomic.LoadInt32(&j.executed) == 1
}

func TestOneOffJobExecution(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	stubJob := &stubOneOffJob{}

	jobsManager.AddOneOff(stubJob)

	jobsManager.RunAll()

	time.Sleep(50 * time.Millisecond)

	if !stubJob.wasExecuted() {
		t.Error("OneOffJob was not executed")
	}
}

func TestPeriodicJobExecution(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	stubJob := &stubPeriodicJob{
		interval: 10 * time.Millisecond,
	}

	jobsManager.Add(stubJob)

	jobsManager.RunAll()

	time.Sleep(stubJob.interval * 10)

	if !stubJob.wasExecuted() {
		t.Error("PeriodicJob was not executed")
	}
}

func TestJobsSetup(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "test-api-key"))

	jobsManager.Setup(mux, cfg)

	if jobsManager.apiKey != "test-api-key" {
		t.Errorf("Expected apiKey to be 'test-api-key', got '%s'", jobsManager.apiKey)
	}
}

func TestHandlePeriodicJobWithAPIKey(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	stubJob := &stubPeriodicJob{
		interval: 10 * time.Millisecond,
	}
	jobsManager.Add(stubJob)

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "test-api-key"))
	jobsManager.Setup(mux, cfg)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/periodic/stubPeriodicJob", nil)
	req.Header.Set(common.HeaderAPIKey, "test-api-key")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if body := w.Body.String(); body != "started" {
		t.Errorf("Expected body 'started', got '%s'", body)
	}
}

func TestHandlePeriodicJobNoAPIKey(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "test-api-key"))
	jobsManager.Setup(mux, cfg)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/periodic/stubPeriodicJob", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestHandlePeriodicJobWrongAPIKey(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "test-api-key"))
	jobsManager.Setup(mux, cfg)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/periodic/stubPeriodicJob", nil)
	req.Header.Set(common.HeaderAPIKey, "wrong-key")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

func TestHandlePeriodicJobNotFound(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "test-api-key"))
	jobsManager.Setup(mux, cfg)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/periodic/nonexistent", nil)
	req.Header.Set(common.HeaderAPIKey, "test-api-key")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandleOneOffJobWithAPIKey(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	stubJob := &stubOneOffJob{}
	jobsManager.AddOneOff(stubJob)

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "test-api-key"))
	jobsManager.Setup(mux, cfg)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/oneoff/stubOneOffJob", nil)
	req.Header.Set(common.HeaderAPIKey, "test-api-key")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if body := w.Body.String(); body != "started" {
		t.Errorf("Expected body 'started', got '%s'", body)
	}
}

func TestHandleOneOffJobNotFound(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "test-api-key"))
	jobsManager.Setup(mux, cfg)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/oneoff/nonexistent", nil)
	req.Header.Set(common.HeaderAPIKey, "test-api-key")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestSecurityMiddlewareNoConfiguredKey(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, ""))
	jobsManager.Setup(mux, cfg)

	req := httptest.NewRequest(http.MethodPost, "/maintenance/periodic/test", nil)
	req.Header.Set(common.HeaderAPIKey, "any-key")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

func TestJobsUpdateConfig(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "updated-key"))

	jobsManager.UpdateConfig(cfg)

	if jobsManager.apiKey != "updated-key" {
		t.Errorf("Expected apiKey to be 'updated-key', got '%s'", jobsManager.apiKey)
	}
}

func TestJobsSpawn(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Shutdown()

	stubJob := &stubPeriodicJob{
		interval: 10 * time.Millisecond,
	}

	jobsManager.Spawn(stubJob)

	time.Sleep(50 * time.Millisecond)

	if !stubJob.wasExecuted() {
		t.Error("Spawned job was not executed")
	}
}
