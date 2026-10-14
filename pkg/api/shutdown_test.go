package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/medama-io/go-useragent"
)

func shutdownFormServer(t *testing.T, endpoint string) (*Server, *sync.WaitGroup) {
	t.Helper()
	form := &dbgen.Form{
		ID:         123,
		ExternalID: db.TestPropertyUUID,
		URL:        endpoint,
		Method:     dbgen.FormMethodPost,
		Enabled:    true,
		Active:     true,
	}
	store := db.NewBusinessWithQuerier(nil, &formProxyQuerierStub{
		QuerierStub: &db.QuerierStub{},
		forms:       []*dbgen.Form{form},
	}, db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{}))
	metrics := monitoring.NewStub()
	parser := useragent.NewParser()
	srv := &Server{
		BusinessDB:        store,
		TimeSeries:        db.NewMemoryTimeSeries(),
		Levels:            difficulty.NewLevels(db.NewMemoryTimeSeries(), 10, PropertyBucketSize),
		Auth:              NewAuthMiddleware(store, nil, billing.NewPlanService(nil), metrics, rules.NewRulesCompiler(parser)),
		Verifier:          NewVerifier(testsConfigStore(), store, testsConfigStore().Get(common.FingerprintHeaderKey), parser),
		VerifyLogChan:     make(chan *common.VerifyRecord, 10),
		FormSubmitLogChan: make(chan *common.FormSubmitRecord, 10),
		FormURLVerifier:   AllowAllFormURLVerifier{},
		Metrics:           metrics,
		AsyncTasks:        maintenance.NewAsyncTasksJob(store),
	}
	workers := &sync.WaitGroup{}
	if err := srv.Init(t.Context(), ServerConfig{
		VerifyFlushInterval: time.Hour,
		AuthBackfillDelay:   time.Hour,
		FormFlushInterval:   time.Hour,
		FormQueueCapacity:   10 * FormBatchSize,
		BackpressureTimeout: time.Second,
	}, workers); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := srv.SubmitForm(t.Context(), &FormSubmission{
			FormExternalID: db.UUIDToString(form.ExternalID),
			Values:         url.Values{"message": {"queued"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(srv.Stop)
	return srv, workers
}

func TestServerShutdownDrainsQueuedForms(t *testing.T) {
	started := make(chan struct{}, 3)
	canceled := make(chan struct{}, 3)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var delivered atomic.Int32
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
			delivered.Add(1)
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			canceled <- struct{}{}
		}
	}))
	defer func() { unblock(); downstream.Close() }()
	srv, workers := shutdownFormServer(t, downstream.URL)
	srv.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- common.WaitWorkers(ctx, workers) }()
	for range 3 {
		select {
		case <-started:
		case <-done:
			t.Fatal("shutdown finished before queued forms were delivered")
		case <-time.After(time.Second):
			t.Fatal("queued form URL was not called")
		}
	}
	select {
	case <-canceled:
		t.Fatal("form delivery was canceled during draining")
	case <-done:
		t.Fatal("shutdown did not wait for form delivery")
	case <-time.After(20 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after form delivery")
	}
	if count := delivered.Load(); count != 3 {
		t.Fatalf("delivered %d of 3 forms", count)
	}
}

func TestServerShutdownUsesExistingBatchFlushTimeout(t *testing.T) {
	started := make(chan struct{}, 3)
	downstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer downstream.Close()
	srv, workers := shutdownFormServer(t, downstream.URL)
	srv.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- common.WaitWorkers(ctx, workers) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("form delivery did not start")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker completion: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("existing final batch flush exceeded its timeout")
	}
}

func TestServerStopRejectsFormSubmissions(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()
	srv, workers := shutdownFormServer(t, downstream.URL)
	srv.Stop()
	srv.Stop()
	if err := srv.SubmitForm(t.Context(), &FormSubmission{ID: "late"}); err != ErrFormQueueStopped {
		t.Fatalf("submission after Stop: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := common.WaitWorkers(ctx, workers); err != nil {
		t.Fatal(err)
	}
}

func TestServerWaitWorkersDoesNotStop(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()
	srv, workers := shutdownFormServer(t, downstream.URL)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := common.WaitWorkers(ctx, workers); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait result: %v", err)
	}
	if err := srv.SubmitForm(t.Context(), &FormSubmission{
		FormExternalID: db.UUIDToString(db.TestPropertyUUID),
	}); err != nil {
		t.Fatalf("waiting stopped form admission: %v", err)
	}
	srv.Stop()
	waitCtx, stopWait := context.WithTimeout(t.Context(), time.Second)
	defer stopWait()
	if err := common.WaitWorkers(waitCtx, workers); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitFormRejectsNilSubmission(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()
	srv, workers := shutdownFormServer(t, downstream.URL)
	if err := srv.SubmitForm(t.Context(), nil); err != errInvalidArg {
		t.Fatalf("invalid submission: %v", err)
	}
	srv.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := common.WaitWorkers(ctx, workers); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitFormConcurrentStop(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()
	srv, workers := shutdownFormServer(t, downstream.URL)
	start := make(chan struct{})
	errorsChan := make(chan error, 100)
	var senders sync.WaitGroup
	for range 100 {
		senders.Go(func() {
			<-start
			errorsChan <- srv.SubmitForm(t.Context(), &FormSubmission{
				FormExternalID: db.UUIDToString(db.TestPropertyUUID),
			})
		})
	}
	close(start)
	srv.Stop()
	senders.Wait()
	for range 100 {
		if err := <-errorsChan; err != nil && err != ErrFormQueueStopped {
			t.Fatalf("submission racing Stop: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := common.WaitWorkers(ctx, workers); err != nil {
		t.Fatal(err)
	}
}

func TestServerStopCancelsIndependentWorkersBeforeFormsFinish(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer func() { unblock(); downstream.Close() }()
	srv, workers := shutdownFormServer(t, downstream.URL)
	authStopped := make(chan struct{})
	var stoppedOnce sync.Once
	cancelAuth := srv.Auth.SitekeyBackfillCancel
	srv.Auth.SitekeyBackfillCancel = func() {
		cancelAuth()
		stoppedOnce.Do(func() { close(authStopped) })
	}
	srv.Stop()
	select {
	case <-authStopped:
	default:
		t.Fatal("auth backfills remained tied to form delivery")
	}
	unblock()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := common.WaitWorkers(ctx, workers); err != nil {
		t.Fatal(err)
	}
}
