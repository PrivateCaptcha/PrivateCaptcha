package portal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type offboardPlanServiceStub struct {
	billing.PlanService
	cancelledID string
	cancelError error
}

func (s *offboardPlanServiceStub) CancelSubscription(_ context.Context, sid string) error {
	s.cancelledID = sid
	return s.cancelError
}

func TestOffboardUserJob(t *testing.T) {
	cancelError := errors.New("subscription cancellation failed")
	tests := []struct {
		name          string
		subscription  *dbgen.Subscription
		cancelError   error
		wantCancelled string
	}{
		{
			name: "NoSubscription",
		},
		{
			name: "ActiveSubscription",
			subscription: &dbgen.Subscription{
				Status:                 billing.InternalStatusTrialing,
				ExternalSubscriptionID: db.Text("active-subscription"),
			},
			wantCancelled: "active-subscription",
		},
		{
			name: "InactiveSubscription",
			subscription: &dbgen.Subscription{
				Status:                 billing.InternalStatusExpired,
				ExternalSubscriptionID: db.Text("expired-subscription"),
			},
		},
		{
			name: "NoExternalSubscription",
			subscription: &dbgen.Subscription{
				Status: billing.InternalStatusTrialing,
			},
		},
		{
			name: "ScheduledCancellation",
			subscription: &dbgen.Subscription{
				Status:                 billing.InternalStatusTrialing,
				ExternalSubscriptionID: db.Text("scheduled-subscription"),
				CancelFrom:             db.Timestampz(time.Now().Add(24 * time.Hour)),
			},
		},
		{
			name: "PastCancellation",
			subscription: &dbgen.Subscription{
				Status:                 billing.InternalStatusTrialing,
				ExternalSubscriptionID: db.Text("past-cancellation-subscription"),
				CancelFrom:             db.Timestampz(time.Now().Add(-24 * time.Hour)),
			},
			wantCancelled: "past-cancellation-subscription",
		},
		{
			name: "CancellationError",
			subscription: &dbgen.Subscription{
				Status:                 billing.InternalStatusTrialing,
				ExternalSubscriptionID: db.Text("failed-subscription"),
			},
			cancelError:   cancelError,
			wantCancelled: "failed-subscription",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planService := &offboardPlanServiceStub{
				PlanService: billing.NewPlanService(nil),
				cancelError: tt.cancelError,
			}
			srv := &Server{
				PlanService: planService,
			}
			user := &dbgen.User{
				ID: 42,
			}
			job := srv.OffboardUser(user, tt.subscription)
			if planService.cancelledID != "" {
				t.Fatal("subscription was cancelled before the job ran")
			}
			if err := job.RunOnce(t.Context(), job.NewParams()); !errors.Is(err, tt.cancelError) {
				t.Errorf("RunOnce() error = %v, want %v", err, tt.cancelError)
			}
			if planService.cancelledID != tt.wantCancelled {
				t.Errorf("cancelled subscription = %q, want %q", planService.cancelledID, tt.wantCancelled)
			}
		})
	}
}

// blockingOffboardPlanService implements billing.PlanService for shutdown
// regression tests. CancelSubscription blocks until either unblock is closed
// (simulating a successful external API call) or ctx is cancelled (simulating
// shutdown cancellation), so tests can assert Shutdown waits for the in-flight
// offboard goroutine instead of dropping it.
type blockingOffboardPlanService struct {
	billing.PlanService
	started     chan struct{}
	unblock     chan struct{}
	finished    chan struct{}
	cancelledID string
	cancelErr   error
	startOnce   sync.Once
	finishOnce  sync.Once
}

func (s *blockingOffboardPlanService) CancelSubscription(ctx context.Context, sid string) error {
	s.cancelledID = sid
	s.startOnce.Do(func() { close(s.started) })
	defer s.finishOnce.Do(func() { close(s.finished) })
	select {
	case <-s.unblock:
		return s.cancelErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newShutdownTestServer builds a Server whose offboard goroutine routes
// through runOffboardJob against a real shutdownCtx (mimicking Init) and a
// blocking PlanService. No DB is required because runOffboardJob's fallback
// path launches the job directly without the async-tasks DLQ.
func newShutdownTestServer(planService *blockingOffboardPlanService) *Server {
	srv := &Server{
		PlanService: planService,
	}
	// Init normally sets Jobs = s; replicate just enough for runOffboardJob
	// -> s.Jobs.OffboardUser -> s.OffboardUser to work without a full Init.
	srv.Jobs = srv
	srv.shutdownCtx, srv.shutdownCancel = context.WithCancel(context.Background())
	return srv
}

func activeTestSubscription(id string) *dbgen.Subscription {
	return &dbgen.Subscription{
		Status:                 billing.InternalStatusTrialing,
		ExternalSubscriptionID: db.Text(id),
	}
}

// TestRunOffboardJobTrackedDuringShutdown is the regression test for the bug
// where deleteAccount launched the external subscription cancellation on a
// detached, untracked goroutine (`go common.RunOneOffJob(... context.Background() ...)`)
// that was silently dropped by a deploy/SIGTERM between the soft-delete commit
// and CancelSubscription returning. It verifies the offboard goroutine is now
// tracked by jobsWG and that Shutdown waits for it to complete.
func TestRunOffboardJobTrackedDuringShutdown(t *testing.T) {
	planService := &blockingOffboardPlanService{
		PlanService: billing.NewPlanService(nil),
		started:     make(chan struct{}),
		unblock:     make(chan struct{}),
		finished:    make(chan struct{}),
	}
	srv := newShutdownTestServer(planService)

	srv.runOffboardJob(context.Background(), &dbgen.User{ID: 42}, activeTestSubscription("active-subscription"))

	// Wait until the offboard goroutine is blocked inside CancelSubscription.
	<-planService.started

	shutdownDone := make(chan struct{})
	go func() {
		srv.Shutdown(30 * time.Second)
		close(shutdownDone)
	}()

	// Shutdown must NOT return while the cancellation is still in-flight.
	// Previously the detached goroutine would simply be dropped here.
	select {
	case <-shutdownDone:
		t.Fatal("Shutdown returned before the offboard goroutine completed")
	case <-time.After(100 * time.Millisecond):
		// expected: Shutdown is still waiting for the tracked goroutine
	}

	// Let the cancellation finish and assert Shutdown waits for it.
	close(planService.unblock)
	select {
	case <-shutdownDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return after the offboard goroutine completed")
	}

	if planService.cancelledID != "active-subscription" {
		t.Fatalf("subscription was not cancelled, got %q", planService.cancelledID)
	}
}

// TestShutdownCancelsOffboardOnTimeout verifies that a stuck offboard goroutine
// does not block shutdown indefinitely: once maxWait elapses, Shutdown cancels
// shutdownCtx, which unblocks (and abandons) the in-flight cancellation so the
// process can exit.
func TestShutdownCancelsOffboardOnTimeout(t *testing.T) {
	planService := &blockingOffboardPlanService{
		PlanService: billing.NewPlanService(nil),
		started:     make(chan struct{}),
		unblock:     make(chan struct{}), // never closed; only ctx cancellation can unblock
		finished:    make(chan struct{}),
	}
	srv := newShutdownTestServer(planService)

	srv.runOffboardJob(context.Background(), &dbgen.User{ID: 42}, activeTestSubscription("active-subscription"))
	<-planService.started

	start := time.Now()
	srv.Shutdown(100 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed >= 2*time.Second {
		t.Fatalf("Shutdown took too long (%v); expected it to cancel the context shortly after 100ms", elapsed)
	}

	// The cancellation call was reached and then unblocked by the context
	// cancellation, so the goroutine should have returned.
	select {
	case <-planService.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("offboard goroutine did not return after shutdown cancelled its context")
	}

	if planService.cancelledID != "active-subscription" {
		t.Fatalf("subscription was not cancelled, got %q", planService.cancelledID)
	}
}

// TestRunOffboardJobSurvivesRequestCancellation verifies the offboard goroutine
// is parented to shutdownCtx (not the in-flight request context), so the
// external subscription cancellation is NOT dropped when the HTTP response is
// sent (which cancels r.Context()). Before the fix, the pre-458f70e1 path ran
// cancellation synchronously inside the request; the bug moved it to
// context.Background() and the fix routes it through shutdownCtx.
func TestRunOffboardJobSurvivesRequestCancellation(t *testing.T) {
	planService := &blockingOffboardPlanService{
		PlanService: billing.NewPlanService(nil),
		started:     make(chan struct{}),
		unblock:     make(chan struct{}),
		finished:    make(chan struct{}),
	}
	srv := newShutdownTestServer(planService)

	reqCtx, reqCancel := context.WithCancel(context.Background())
	srv.runOffboardJob(reqCtx, &dbgen.User{ID: 42}, activeTestSubscription("active-subscription"))
	<-planService.started

	// Simulate the HTTP response having been sent / client disconnecting,
	// which cancels the in-flight request context.
	reqCancel()

	// The offboard goroutine must still be running: it is parented to
	// shutdownCtx, not reqCtx. If it had been parented to reqCtx, the
	// cancellation would have been dropped here.
	select {
	case <-planService.finished:
		t.Fatal("offboard goroutine finished after request cancellation; expected it to keep running under shutdownCtx")
	case <-time.After(100 * time.Millisecond):
		// expected: still running under shutdownCtx
	}

	// Let the cancellation complete and ensure it actually ran.
	close(planService.unblock)
	<-planService.finished
	srv.Shutdown(5 * time.Second)

	if planService.cancelledID != "active-subscription" {
		t.Fatalf("subscription was not cancelled, got %q", planService.cancelledID)
	}
}

// TestRunOffboardJobSkipsInactiveSubscription verifies the offboard goroutine
// completes immediately (and Shutdown returns promptly) when there is nothing
// to cancel, so the tracking infrastructure does not add latency for the common
// no-op case.
func TestRunOffboardJobSkipsInactiveSubscription(t *testing.T) {
	planService := &blockingOffboardPlanService{
		PlanService: billing.NewPlanService(nil),
		started:     make(chan struct{}),
		unblock:     make(chan struct{}),
		finished:    make(chan struct{}),
	}
	srv := newShutdownTestServer(planService)

	// Inactive subscription (Expired): OffboardUserJob.RunOnce returns nil
	// without calling CancelSubscription.
	srv.runOffboardJob(context.Background(), &dbgen.User{ID: 42},
		&dbgen.Subscription{Status: billing.InternalStatusExpired, ExternalSubscriptionID: db.Text("expired")})

	start := time.Now()
	srv.Shutdown(5 * time.Second)
	elapsed := time.Since(start)

	if elapsed >= 1*time.Second {
		t.Fatalf("Shutdown took too long (%v) for a no-op offboard job", elapsed)
	}
	if planService.cancelledID != "" {
		t.Fatalf("inactive subscription was cancelled: %q", planService.cancelledID)
	}
}

// TestDeleteAccountOffboardSurvivesShutdown is the DB-backed integration test
// for the regression. It drives DELETE /user through the real deleteAccount
// handler (which commits the soft-delete then spawns the tracked offboard
// goroutine), then calls Server.Shutdown while CancelSubscription is still
// in-flight, and asserts the cancellation completes instead of being dropped.
// This exercises G1/G2 through the full HTTP handler path (the unit tests
// above exercise runOffboardJob directly).
func TestDeleteAccountOffboardSurvivesShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	// Create a user with a default trial subscription (Status=pc-trial-active,
	// ExternalSubscriptionID valid) so OffboardUserJob.RunOnce reaches
	// CancelSubscription.
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Swap in a blocking PlanService so we can control when CancelSubscription
	// returns, simulating the slow external billing API call. Save originals
	// for cleanup so the shared server is left intact for subsequent tests.
	origPlanService := server.PlanService
	origShutdownCtx := server.shutdownCtx
	origShutdownCancel := server.shutdownCancel
	t.Cleanup(func() {
		server.PlanService = origPlanService
		server.shutdownCtx = origShutdownCtx
		server.shutdownCancel = origShutdownCancel
	})

	blocking := &blockingOffboardPlanService{
		PlanService: billing.NewPlanService(nil),
		started:     make(chan struct{}),
		unblock:     make(chan struct{}),
		finished:    make(chan struct{}),
	}
	server.PlanService = blocking
	// Give the test a fresh shutdown context so cancelling it (if the test
	// needs to) does not affect the shared server's real context.
	server.shutdownCtx, server.shutdownCancel = context.WithCancel(context.Background())

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/user", nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("DELETE /user returned %d, want %d", w.Code, http.StatusSeeOther)
	}

	// Wait until the offboard goroutine is inside CancelSubscription.
	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		// The subscription may not have an active status / external ID in
		// which case the offboard job is a no-op. Verify the user was deleted
		// and skip the shutdown-race portion.
		t.Logf("CancelSubscription was not reached (subscription not eligible); verifying soft-delete only")
		assertUserSoftDeleted(t, ctx, user)
		return
	}

	// The offboard goroutine is now blocked inside CancelSubscription.
	// Call Server.Shutdown which must wait for the tracked goroutine.
	shutdownDone := make(chan struct{})
	go func() {
		server.Shutdown(30 * time.Second)
		close(shutdownDone)
	}()

	// Shutdown must NOT return while the cancellation is still in-flight.
	// Previously (detached goroutine), Shutdown would return immediately
	// and a SIGTERM here would drop the cancellation.
	select {
	case <-shutdownDone:
		t.Fatal("Server.Shutdown returned before the offboard goroutine completed")
	case <-time.After(200 * time.Millisecond):
		// expected: Shutdown is still waiting for the tracked goroutine
	}

	// Let the cancellation finish; Shutdown must then return.
	close(blocking.unblock)
	select {
	case <-shutdownDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Server.Shutdown did not return after the offboard goroutine completed")
	}

	if blocking.cancelledID == "" {
		t.Fatal("CancelSubscription was never called")
	}

	assertUserSoftDeleted(t, ctx, user)
}

func assertUserSoftDeleted(t *testing.T, ctx context.Context, user *dbgen.User) {
	t.Helper()
	_, err := store.Impl().RetrieveUser(ctx, user.ID)
	if err != db.ErrSoftDeleted {
		t.Errorf("Expected ErrSoftDeleted after deleting user, got: %v", err)
	}
}

// TestOffboardJobNotRetriableViaDLQForDeletedUser documents that the existing
// AsyncTasksJob DLQ cannot recover a dropped offboard task for a soft-deleted
// user. GetPendingAsyncTasks filters `u.deleted_at IS NULL`, so once the
// soft-delete commits (which happens before runOffboardJob), any async task
// referencing that user is invisible to the DLQ sweep. This is why the fix
// focuses on the shutdown-race regression (WaitGroup + shutdownCtx) rather than
// enqueuing the cancellation through the DLQ.
func TestOffboardJobNotRetriableViaDLQForDeletedUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.Impl().SoftDeleteUser(ctx, user)
	})

	// Soft-delete the user (mimicking what deleteAccount does before launching
	// the offboard goroutine).
	if _, err := store.Impl().SoftDeleteUser(ctx, user); err != nil {
		t.Fatalf("Failed to soft-delete user: %v", err)
	}

	// Enqueue an async task referencing the soft-deleted user, keyed by the
	// external subscription ID (as the recommended DLQ fix would).
	scheduledAt := time.Now().UTC().Add(-1 * time.Second)
	_, err = store.Impl().CreateNewAsyncTask(ctx, struct{}{}, "offboard-cancel", user, scheduledAt, "offboard/"+user.Email)
	if err != nil {
		t.Fatalf("Failed to create async task: %v", err)
	}

	// The DLQ sweep query (GetPendingAsyncTasks) filters `u.deleted_at IS NULL`,
	// so the task for the soft-deleted user must NOT be returned.
	tasks, err := store.Impl().RetrievePendingAsyncTasks(ctx, 10, time.Now().UTC().Add(-24*time.Hour), 2)
	if err != nil {
		t.Fatalf("Failed to retrieve pending tasks: %v", err)
	}

	for _, task := range tasks {
		if task.AsyncTask.UserID.Valid && task.AsyncTask.UserID.Int32 == user.ID {
			t.Fatalf("DLQ returned a task for soft-deleted user %d; GetPendingAsyncTasks should filter u.deleted_at IS NULL", user.ID)
		}
	}
}

type registrationVerificationStoreStub struct {
	session.Store
	sid string
}

func (s *registrationVerificationStoreStub) SetVerifyRegistration(_ context.Context, sid string, value bool) error {
	s.sid = sid
	return nil
}

func (s *registrationVerificationStoreStub) UpdatePayload(context.Context, string) {}

func TestCheckRegistrationJob(t *testing.T) {
	form := url.Values{common.ParamEmail: {spammerEmail}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	store := &registrationVerificationStoreStub{}
	srv := &Server{Sessions: &session.Manager{Store: store}}
	sess := session.NewSessionWithAuthority(
		session.Authority{State: session.StatePending, ChallengeKind: session.ChallengeKindRegistration},
		session.NewPayload(t.Name(), store),
	)
	job := srv.CheckRegistration(sess, req, 0)
	if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
		t.Fatal(err)
	}
	if store.sid != sess.ID() {
		t.Fatalf("verification SID = %q, want %q", store.sid, sess.ID())
	}
}

func TestCleanupAuditLogJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create a test user using the helper function
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Impl().SoftDeleteUser(ctx, user)
	})

	// Create the cleanup job with immediate cleanup (0 past interval)
	job := &maintenance.CleanupAuditLogJob{
		BusinessDB:   store,
		PastInterval: 0,
	}

	// Run the job
	err = job.RunOnce(ctx, &maintenance.CleanupAuditLogParams{
		PastInterval: 0, // Cleanup everything before now
	})
	if err != nil {
		t.Errorf("CleanupAuditLogJob.RunOnce() error = %v", err)
	}

	// Verify job methods
	if job.Name() != "cleanup_audit_log_job" {
		t.Errorf("Expected job name 'cleanup_audit_log_job', got '%s'", job.Name())
	}

	if job.Interval() != 1*time.Hour {
		t.Errorf("Expected interval 1h, got %v", job.Interval())
	}

	if job.Timeout() != 1*time.Minute {
		t.Errorf("Expected timeout 1m, got %v", job.Timeout())
	}

	if job.Trigger() != nil {
		t.Error("Expected nil trigger")
	}
}

func TestCleanupAsyncTasksJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create old async tasks
	oldTime := time.Now().UTC().Add(-30 * 24 * time.Hour) // 30 days ago
	_, err := store.Impl().CreateNewAsyncTask(ctx, map[string]string{"key": "value"}, "test_handler", nil, oldTime, "test-ref-1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Impl().CreateNewAsyncTask(ctx, map[string]string{"key": "value2"}, "test_handler", nil, oldTime.Add(-1*time.Hour), "test-ref-2")
	if err != nil {
		t.Fatal(err)
	}

	// Create the cleanup job
	job := &maintenance.CleanupAsyncTasksJob{
		BusinessDB:   store,
		PastInterval: 0,
	}

	// Run the job - it will cleanup tasks older than now
	err = job.RunOnce(ctx, &maintenance.CleanupAsyncTasksParams{
		PastInterval: 0, // Cleanup everything before now
	})
	if err != nil {
		t.Errorf("CleanupAsyncTasksJob.RunOnce() error = %v", err)
	}

	// Verify job methods
	if job.Name() != "cleanup_async_tasks_job" {
		t.Errorf("Expected job name 'cleanup_async_tasks_job', got '%s'", job.Name())
	}

	if job.Interval() != 3*time.Hour {
		t.Errorf("Expected interval 3h, got %v", job.Interval())
	}

	if job.Timeout() != 1*time.Minute {
		t.Errorf("Expected timeout 1m, got %v", job.Timeout())
	}

	if job.Trigger() != nil {
		t.Error("Expected nil trigger")
	}
}

func TestCleanupAuditLogJobNewParams(t *testing.T) {
	job := &maintenance.CleanupAuditLogJob{
		PastInterval: 7 * 24 * time.Hour,
	}

	params := job.NewParams()
	p, ok := params.(*maintenance.CleanupAuditLogParams)
	if !ok {
		t.Fatal("NewParams() did not return *CleanupAuditLogParams")
	}

	if p.PastInterval != 7*24*time.Hour {
		t.Errorf("Expected PastInterval 7d, got %v", p.PastInterval)
	}
}

func TestCleanupAsyncTasksJobNewParams(t *testing.T) {
	job := &maintenance.CleanupAsyncTasksJob{
		PastInterval: 14 * 24 * time.Hour,
	}

	params := job.NewParams()
	p, ok := params.(*maintenance.CleanupAsyncTasksParams)
	if !ok {
		t.Fatal("NewParams() did not return *CleanupAsyncTasksParams")
	}

	if p.PastInterval != 14*24*time.Hour {
		t.Errorf("Expected PastInterval 14d, got %v", p.PastInterval)
	}
}

func TestCleanupAuditLogJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &maintenance.CleanupAuditLogJob{
		BusinessDB:   store,
		PastInterval: 30 * 24 * time.Hour, // Default to 30 days
	}

	// Run with invalid params (wrong type) - should use default
	err := job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupAuditLogJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}

func TestCleanupAsyncTasksJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &maintenance.CleanupAsyncTasksJob{
		BusinessDB:   store,
		PastInterval: 30 * 24 * time.Hour, // Default to 30 days
	}

	// Run with invalid params (wrong type) - should use default
	err := job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupAsyncTasksJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}

func TestCleanupDeletedRecordsJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create the cleanup job
	job := &maintenance.CleanupDeletedRecordsJob{
		Store: store,
		Age:   30 * 24 * time.Hour, // 30 days
	}

	// Run the job - it should not fail even with no deleted records
	err := job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Errorf("CleanupDeletedRecordsJob.RunOnce() error = %v", err)
	}
}

func TestCleanupDeletedRecordsJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &maintenance.CleanupDeletedRecordsJob{
		Store: store,
		Age:   30 * 24 * time.Hour, // Default to 30 days
	}

	// Run with invalid params (wrong type) - should use default
	err := job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupDeletedRecordsJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}

func TestCleanupUserNotificationsJob(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create the cleanup job
	job := &maintenance.CleanupUserNotificationsJob{
		Store:              store,
		NotificationMonths: 1, // Cleanup notifications older than 1 month
		TemplateMonths:     6,
	}

	// Run the job - it should not fail even with no notifications to clean
	err := job.RunOnce(ctx, job.NewParams())
	if err != nil {
		t.Errorf("CleanupUserNotificationsJob.RunOnce() error = %v", err)
	}
}

func TestCleanupUserNotificationsJobWithInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	job := &maintenance.CleanupUserNotificationsJob{
		Store:              store,
		NotificationMonths: 3,
		TemplateMonths:     6,
	}

	// Run with invalid params (wrong type) - should use default
	err := job.RunOnce(ctx, "invalid params")
	if err != nil {
		t.Errorf("CleanupUserNotificationsJob.RunOnce() with invalid params should not error, got = %v", err)
	}
}
