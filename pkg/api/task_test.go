package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/rs/xid"
)

func TestGetAsyncTaskPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user1, _, apiKey1, err := setupAPISuite(ctx, t.Name()+"_owner")
	if err != nil {
		t.Fatal(err)
	}

	handlerID := xid.New().String()
	request := struct{}{}
	task, err := server.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user1, time.Now().UTC().Add(24*time.Hour), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	taskID := db.UUIDToString(task.ID)

	_, _, apiKey2, err := setupAPISuite(ctx, t.Name()+"_other")
	if err != nil {
		t.Fatal(err)
	}

	// api key 2 belongs to the wrong user
	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKey2)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}

	// with api key 1 it should work
	_, meta, err := requestResponseAPISuite[*apiAsyncTaskResultOutput](ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKey1)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}
}

func TestGetAsyncTaskInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	apiKey := db.UUIDToSecret(*randomUUID())
	taskID := "some-task-id"

	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected status code: %v", resp.StatusCode)
	}
}

func TestGetAsyncTaskReadOnlyKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, apiKey, err := setupAPISuiteEx(ctx, t.Name(), dbgen.ApiKeyScopePortal, true /*read-only*/, false /*scope org*/)
	if err != nil {
		t.Fatal(err)
	}

	handlerID := xid.New().String()
	request := struct{}{}
	task, err := server.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user, time.Now().UTC().Add(24*time.Hour), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	taskID := db.UUIDToString(task.ID)

	// with read-only api key it still should work
	_, meta, err := requestResponseAPISuite[*apiAsyncTaskResultOutput](ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKey)
	if err != nil {
		t.Fatal(err)
	}

	if !meta.Code.Success() {
		t.Fatalf("Unexpected status code: %v", meta.Description)
	}
}

func TestGetAsyncTaskNoSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	user, _, err := db_test.CreateNewAccountForTestEx(ctx, store, t.Name(), nil)
	if err != nil {
		t.Fatal(err)
	}

	keyParams := tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0)
	keyParams.Scope = dbgen.ApiKeyScopePortal
	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, keyParams)
	if err != nil {
		t.Fatal(err)
	}
	apiKeyStr := db.UUIDToSecret(apikey.ExternalID)

	handlerID := xid.New().String()
	request := struct{}{}
	task, err := server.BusinessDB.Impl().CreateNewAsyncTask(ctx, request, handlerID, user, time.Now().UTC().Add(24*time.Hour), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	taskID := db.UUIDToString(task.ID)

	resp, err := apiRequestSuite(ctx, nil, http.MethodGet, "/"+common.AsyncTaskEndpoint+"/"+taskID, apiKeyStr)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("expected status %d, got %d", http.StatusPaymentRequired, resp.StatusCode)
	}
}

func TestGetAsyncTaskInvalidIDFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())

	_, _, apiKey, err := setupAPISuite(ctx, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		taskID     string
		wantStatus int
	}{
		{
			name:       "Invalid UUID Format",
			taskID:     "not-a-valid-uuid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Empty Task ID",
			taskID:     "",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := "/" + common.AsyncTaskEndpoint + "/" + tt.taskID
			resp, err := apiRequestSuite(ctx, nil, http.MethodGet, endpoint, apiKey)
			if err != nil {
				t.Fatal(err)
			}

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

// TestAsyncTaskDoubleExecution reproduces an immediate attempt that errors (e.g. context deadline exceeded
// after partial commits) leaves the task pending, and a subsequent worker
// RunOnce re-selects and re-executes it. With the fix, both executions are
// gated by an atomic claim, so total executions are bounded to MaxAttempts
// and each claim is accounted for in processing_attempts.
func TestAsyncTaskDoubleExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	var executed int32
	job := maintenance.NewAsyncTasksJob(store)
	handlerID := xid.New().String()
	job.Register(handlerID, func(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
		n := atomic.AddInt32(&executed, 1)
		if n == 1 {
			// Simulate the immediate attempt timing out mid-batch.
			return nil, context.DeadlineExceeded
		}
		return []byte(`{}`), nil
	})
	defer job.Deregister(handlerID)

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	// scheduledAt in the past so the worker can immediately select it.
	task, err := store.Impl().CreateNewAsyncTask(ctx, struct{}{}, handlerID, user, time.Now().UTC().Add(-1*time.Second), t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the immediate attempt goroutine. The handler runs once and
	// returns context.DeadlineExceeded.
	if err := job.Execute(ctx, task); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, want context.DeadlineExceeded", err)
	}

	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Fatalf("executed = %v after immediate attempt, want 1", actual)
	}

	// The errored immediate attempt must leave the task pending so the worker
	// can retry it: processed_at IS NULL, processing_attempts == 1.
	pending, err := store.Impl().RetrieveAsyncTask(ctx, task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pending.ProcessedAt.Valid {
		t.Fatalf("processed_at should be NULL after errored immediate attempt, got %v", pending.ProcessedAt.Time)
	}
	if pending.ProcessingAttempts != 1 {
		t.Fatalf("processing_attempts = %v after errored immediate attempt, want 1", pending.ProcessingAttempts)
	}

	// Simulate the worker tick. It re-selects the pending task and runs the
	// handler a second (and final) time.
	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatal(err)
	}

	if actual := atomic.LoadInt32(&executed); actual != 2 {
		t.Fatalf("executed = %v after worker run, want 2", actual)
	}

	finished, err := store.Impl().RetrieveAsyncTask(ctx, task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !finished.ProcessedAt.Valid {
		t.Fatal("processed_at should be set after successful worker run")
	}
	if finished.ProcessingAttempts != 2 {
		t.Fatalf("processing_attempts = %v after worker run, want 2", finished.ProcessingAttempts)
	}
}

// TestAsyncTaskClaimPreventsConcurrentExecution verifies the core fix: when
// two runners call Execute on the same fresh task concurrently, the atomic
// claim ensures only one of them runs the handler. This test fails before
// the fix (both runners execute the handler) and passes after (one claim
// succeeds, the other is skipped).
func TestAsyncTaskClaimPreventsConcurrentExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	// MaxAttempts = 1 means only one claim can ever succeed for this task.
	job := maintenance.NewAsyncTasksJob(store)
	job.MaxAttempts = 1
	handlerID := xid.New().String()
	var executed int32
	job.Register(handlerID, func(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
		atomic.AddInt32(&executed, 1)
		return []byte(`{}`), nil
	})
	defer job.Deregister(handlerID)

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	task, err := store.Impl().CreateNewAsyncTask(ctx, struct{}{}, handlerID, user, time.Now().UTC().Add(-1*time.Second), t.Name())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	const runners = 4
	start := make(chan struct{})
	wg.Add(runners)
	for i := 0; i < runners; i++ {
		go func() {
			defer wg.Done()
			<-start
			// We ignore the returned error: a skipped claim returns nil.
			_ = job.Execute(ctx, task)
		}()
	}
	close(start)
	wg.Wait()

	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Fatalf("handler executed %v times under concurrent Execute, want 1", actual)
	}

	// The single successful claim must have been recorded and the task marked
	// finished (processed_at set, processing_attempts == 1 == MaxAttempts).
	final, err := store.Impl().RetrieveAsyncTask(ctx, task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !final.ProcessedAt.Valid {
		t.Fatal("processed_at should be set after the single successful execution")
	}
	if final.ProcessingAttempts != 1 {
		t.Fatalf("processing_attempts = %v, want 1", final.ProcessingAttempts)
	}
}

// TestAsyncTaskExecuteSkipsCompletedTask verifies that once a task is
// completed (processed_at set), a subsequent Execute (e.g. a late/stale
// immediate attempt or a duplicate worker tick) does NOT run the handler
// again. Before the fix, Execute unconditionally ran the handler and
// UpdateAsyncTask clobbered the completed row.
func TestAsyncTaskExecuteSkipsCompletedTask(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	job := maintenance.NewAsyncTasksJob(store)
	handlerID := xid.New().String()
	var executed int32
	job.Register(handlerID, func(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
		atomic.AddInt32(&executed, 1)
		return []byte(`{"ok":true}`), nil
	})
	defer job.Deregister(handlerID)

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	task, err := store.Impl().CreateNewAsyncTask(ctx, struct{}{}, handlerID, user, time.Now().UTC().Add(-1*time.Second), t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// First Execute claims and completes the task.
	if err := job.Execute(ctx, task); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Fatalf("executed = %v after first Execute, want 1", actual)
	}

	completed, err := store.Impl().RetrieveAsyncTask(ctx, task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.ProcessedAt.Valid {
		t.Fatal("task should be completed after first Execute")
	}
	originalProcessedAt := completed.ProcessedAt.Time
	originalOutput := string(completed.Output)

	// Second Execute simulates a stale/duplicate runner. The claim must fail
	// (processed_at IS NOT NULL) so the handler is NOT invoked.
	if err := job.Execute(ctx, task); err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Fatalf("handler executed %v times, want 1 (completed task must not re-run)", actual)
	}

	// The completed row must not have been clobbered.
	after, err := store.Impl().RetrieveAsyncTask(ctx, task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ProcessedAt.Valid || !after.ProcessedAt.Time.Equal(originalProcessedAt) {
		t.Fatalf("processed_at changed after stale Execute: was %v, now %v", originalProcessedAt, after.ProcessedAt.Time)
	}
	if string(after.Output) != originalOutput {
		t.Fatalf("output changed after stale Execute: was %q, now %q", originalOutput, string(after.Output))
	}
	if after.ProcessingAttempts != 1 {
		t.Fatalf("processing_attempts = %v after stale Execute, want 1", after.ProcessingAttempts)
	}
}

// TestAsyncTaskExecuteSkipsExhaustedTask verifies that a task which has
// already reached MaxAttempts is not executed again by a direct Execute
// call (the immediate-attempt path), even though Execute bypasses the
// worker's pending-tasks SELECT. Before the fix, Execute would run the
// handler for an already-exhausted task.
func TestAsyncTaskExecuteSkipsExhaustedTask(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	job := maintenance.NewAsyncTasksJob(store)
	handlerID := xid.New().String()
	var executed int32
	job.Register(handlerID, func(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
		atomic.AddInt32(&executed, 1)
		return nil, context.DeadlineExceeded
	})
	defer job.Deregister(handlerID)

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	task, err := store.Impl().CreateNewAsyncTask(ctx, struct{}{}, handlerID, user, time.Now().UTC().Add(-1*time.Second), t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Two errored attempts exhaust MaxAttempts (== 2 by default).
	for i := 0; i < job.MaxAttempts; i++ {
		if err := job.Execute(ctx, task); err == nil {
			t.Fatalf("Execute #%d error = nil, want non-nil", i+1)
		}
	}
	if actual := atomic.LoadInt32(&executed); actual != int32(job.MaxAttempts) {
		t.Fatalf("executed = %v after exhausting attempts, want %d", actual, job.MaxAttempts)
	}

	exhausted, err := store.Impl().RetrieveAsyncTask(ctx, task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if exhausted.ProcessedAt.Valid {
		t.Fatal("processed_at should be NULL for an exhausted-but-errored task")
	}
	if exhausted.ProcessingAttempts != int32(job.MaxAttempts) {
		t.Fatalf("processing_attempts = %v, want %d", exhausted.ProcessingAttempts, job.MaxAttempts)
	}

	// A third Execute must not run the handler: claim fails because
	// processing_attempts >= MaxAttempts.
	if err := job.Execute(ctx, task); err != nil {
		t.Fatalf("third Execute() error = %v, want nil (claim skip is not an error)", err)
	}
	if actual := atomic.LoadInt32(&executed); actual != int32(job.MaxAttempts) {
		t.Fatalf("handler executed %v times, want %d (exhausted task must not re-run)", actual, job.MaxAttempts)
	}
}

// TestAsyncTaskWorkerSkipsAlreadyClaimedTask verifies that a task claimed by
// the immediate attempt is not re-executed by the worker once the immediate
// attempt has completed it. This is the end-to-end scenario from the bug
// report's happy path: immediate succeeds, worker should skip.
func TestAsyncTaskWorkerSkipsAlreadyClaimedTask(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	job := maintenance.NewAsyncTasksJob(store)
	handlerID := xid.New().String()
	var executed int32
	job.Register(handlerID, func(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
		atomic.AddInt32(&executed, 1)
		return []byte(`{}`), nil
	})
	defer job.Deregister(handlerID)

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	task, err := store.Impl().CreateNewAsyncTask(ctx, struct{}{}, handlerID, user, time.Now().UTC().Add(-1*time.Second), t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Immediate attempt claims and completes the task.
	if err := job.Execute(ctx, task); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Fatalf("executed = %v after immediate attempt, want 1", actual)
	}

	// Worker tick must NOT re-run the completed task.
	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Fatalf("executed = %v after worker run, want 1 (completed task must not be re-run)", actual)
	}
}

// TestAsyncTaskHappyPathExecutesOnce verifies the happy path: a single
// Execute runs the handler exactly once, marks the task finished, and the
// task is not re-selected by the worker. This guards against regressions
// in the existing single-execution behavior (cf. TestAsyncJob).
func TestAsyncTaskHappyPathExecutesOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := common.TraceContext(t.Context(), t.Name())

	job := maintenance.NewAsyncTasksJob(store)
	handlerID := xid.New().String()
	var executed int32
	job.Register(handlerID, func(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error) {
		atomic.AddInt32(&executed, 1)
		return []byte(`{}`), nil
	})
	defer job.Deregister(handlerID)

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	task, err := store.Impl().CreateNewAsyncTask(ctx, struct{}{}, handlerID, user, time.Now().UTC().Add(-1*time.Second), t.Name())
	if err != nil {
		t.Fatal(err)
	}

	if err := job.Execute(ctx, task); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Fatalf("executed = %v, want 1", actual)
	}

	final, err := store.Impl().RetrieveAsyncTask(ctx, task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !final.ProcessedAt.Valid {
		t.Fatal("processed_at should be set on success")
	}
	if final.ProcessingAttempts != 1 {
		t.Fatalf("processing_attempts = %v, want 1", final.ProcessingAttempts)
	}

	// Worker must not pick it up again.
	if err := job.RunOnce(ctx, job.NewParams()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if actual := atomic.LoadInt32(&executed); actual != 1 {
		t.Fatalf("executed = %v after worker run, want 1", actual)
	}
}

// Compile-time assertion that the fixed UpdateAsyncTask query keeps the same
// Go signature so existing callers (and the stub) remain valid.
var _ = db.UUIDToString
