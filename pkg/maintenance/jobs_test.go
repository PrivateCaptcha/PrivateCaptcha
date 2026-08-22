package maintenance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/justinas/alice"
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

type slowOneOffJob struct {
	started   int32
	cancelled int32
}

func (j *slowOneOffJob) Name() string {
	return "slowOneOffJob"
}

func (j *slowOneOffJob) InitialPause() time.Duration {
	return 0
}

func (j *slowOneOffJob) NewParams() any {
	return struct{}{}
}

func (j *slowOneOffJob) RunOnce(ctx context.Context, params any) error {
	atomic.StoreInt32(&j.started, 1)
	select {
	case <-ctx.Done():
		atomic.StoreInt32(&j.cancelled, 1)
	case <-time.After(10 * time.Second):
	}

	return nil
}

func (j *slowOneOffJob) wasCancelled() bool {
	return atomic.LoadInt32(&j.cancelled) == 1
}

func (j *slowOneOffJob) wasStarted() bool {
	return atomic.LoadInt32(&j.started) == 1
}

type stubOnDemandJobParams struct {
	Value string `json:"value"`
}

type onDemandJobExecution struct {
	service string
	value   string
}

type stubOnDemandJob struct {
	executed  chan onDemandJobExecution
	cancelled chan struct{}
}

var _ common.OnDemandJob = (*stubOnDemandJob)(nil)

func (j *stubOnDemandJob) Name() string {
	return "stubOnDemandJob"
}

func (j *stubOnDemandJob) NewParams() any {
	return &stubOnDemandJobParams{}
}

func (j *stubOnDemandJob) RunOnce(ctx context.Context, params any) error {
	jobParams := params.(*stubOnDemandJobParams)
	service, _ := ctx.Value(common.ServiceContextKey).(string)
	j.executed <- onDemandJobExecution{service: service, value: jobParams.Value}
	<-ctx.Done()
	close(j.cancelled)
	return ctx.Err()
}

func TestOneOffJobExecution(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Stop()

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
	defer jobsManager.Stop()

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

func TestHandlePeriodicJobWithAPIKey(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Stop()

	stubJob := &stubPeriodicJob{
		interval: 10 * time.Millisecond,
	}
	jobsManager.Add(stubJob)

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New(common.APIKeyMiddleware("test-api-key")))

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
	defer jobsManager.Stop()

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New(common.APIKeyMiddleware("test-api-key")))

	req := httptest.NewRequest(http.MethodPost, "/maintenance/periodic/stubPeriodicJob", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestHandlePeriodicJobWrongAPIKey(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Stop()

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New(common.APIKeyMiddleware("test-api-key")))

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
	defer jobsManager.Stop()

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New(common.APIKeyMiddleware("test-api-key")))

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
	defer jobsManager.Stop()

	stubJob := &stubOneOffJob{}
	jobsManager.AddOneOff(stubJob)

	mux := http.NewServeMux()
	cfg := config.NewBaseConfig(nil)
	cfg.Add(config.NewStaticValue(common.LocalAPIKeyKey, "test-api-key"))
	jobsManager.Setup(mux, alice.New())

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
	defer jobsManager.Stop()

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New(common.APIKeyMiddleware("test-api-key")))

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
	defer jobsManager.Stop()

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New(common.APIKeyMiddleware("")))

	req := httptest.NewRequest(http.MethodPost, "/maintenance/periodic/test", nil)
	req.Header.Set(common.HeaderAPIKey, "any-key")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

func TestJobsSpawn(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Stop()

	stubJob := &stubPeriodicJob{
		interval: 10 * time.Millisecond,
	}

	jobsManager.Spawn(stubJob)

	time.Sleep(50 * time.Millisecond)

	if !stubJob.wasExecuted() {
		t.Error("Spawned job was not executed")
	}
}

func TestOnDemandOneOffJobIgnoresStop(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	stubJob := &slowOneOffJob{}
	jobsManager.AddOneOff(stubJob)

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New())

	// Trigger job via HTTP handler (on-demand)
	req := httptest.NewRequest(http.MethodPost, "/maintenance/oneoff/slowOneOffJob", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)
	if !stubJob.wasStarted() {
		t.Fatal("Job was not started")
	}

	// Call Stop() - should cancel the job
	jobsManager.Stop()
	time.Sleep(50 * time.Millisecond)

	if !stubJob.wasCancelled() {
		t.Error("On-demand OneOffJob ignored Stop()")
	}
}

func TestOnDemandJobExecution(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Stop()
	stubJob := &stubOnDemandJob{
		executed:  make(chan onDemandJobExecution, 1),
		cancelled: make(chan struct{}),
	}
	jobsManager.AddOnDemand(stubJob)

	jobsManager.RunAll()
	select {
	case <-stubJob.executed:
		t.Fatal("OnDemandJob was executed by RunAll")
	case <-time.After(50 * time.Millisecond):
	}

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New())
	req := httptest.NewRequest(http.MethodPost, "/maintenance/ondemand/stubOnDemandJob", strings.NewReader(`{"value":"requested"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "started" {
		t.Fatalf("Expected body 'started', got '%s'", body)
	}

	select {
	case execution := <-stubJob.executed:
		if execution.value != "requested" {
			t.Errorf("Expected parameter 'requested', got %q", execution.value)
		}
		if execution.service != "maintenance" {
			t.Errorf("Expected maintenance service context, got %q", execution.service)
		}
	case <-time.After(time.Second):
		t.Fatal("OnDemandJob was not executed by HTTP handler")
	}

	jobsManager.Stop()
	select {
	case <-stubJob.cancelled:
	case <-time.After(time.Second):
		t.Fatal("OnDemandJob was not cancelled by Stop")
	}
}

func TestOnDemandJobRejectsOversizedRequest(t *testing.T) {
	jobsManager := NewJobs(nil, 2)
	defer jobsManager.Stop()
	stubJob := &stubOnDemandJob{
		executed:  make(chan onDemandJobExecution, 1),
		cancelled: make(chan struct{}),
	}
	jobsManager.AddOnDemand(stubJob)

	mux := http.NewServeMux()
	jobsManager.Setup(mux, alice.New())
	body := strings.NewReader(`{"value":"requested"}` + strings.Repeat(" ", 256*1024))
	req := httptest.NewRequest(http.MethodPost, "/maintenance/ondemand/stubOnDemandJob", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d", w.Code)
	}

	select {
	case <-stubJob.executed:
		t.Fatal("OnDemandJob was executed with an oversized request")
	case <-time.After(50 * time.Millisecond):
	}
}
