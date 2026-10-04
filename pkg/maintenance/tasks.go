package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

var (
	errHandlerCrashed = errors.New("async task handler crashed")
	errUnknownHandler = errors.New("handler is not register for the task")
)

// AsyncTaskJob is more of a dead-letter-queue rather than of a "real" processing
// because we should be processing tasks almost immediately
type AsyncTasksJob struct {
	mux          sync.Mutex
	Handlers     map[string]db.AsyncTaskHandler
	BusinessDB   db.Implementor
	Count        int
	PastInterval time.Duration
	MaxAttempts  int
	Semaphore    chan struct{}
}

func NewAsyncTasksJob(store db.Implementor) *AsyncTasksJob {
	return &AsyncTasksJob{
		Handlers:     map[string]db.AsyncTaskHandler{},
		BusinessDB:   store,
		Count:        5,
		PastInterval: 24 * time.Hour,
		MaxAttempts:  2,
		Semaphore:    make(chan struct{}, 10), // we allow 10 "immediate" background jobs
	}
}

type AsyncTasksParams struct {
	Count        int           `json:"count"`
	PastInterval time.Duration `json:"past_interval"`
	MaxAttempts  int           `json:"max_attempts"`
}

var _ common.PeriodicJob = (*AsyncTasksJob)(nil)
var _ db.AsyncTasks = (*AsyncTasksJob)(nil)

func (j *AsyncTasksJob) Timeout() time.Duration {
	return j.Interval()
}

func (j *AsyncTasksJob) Trigger() <-chan struct{} { return nil }

func (j *AsyncTasksJob) Register(handler string, fn db.AsyncTaskHandler) bool {
	j.mux.Lock()
	defer j.mux.Unlock()

	if _, ok := j.Handlers[handler]; ok {
		return false
	}

	j.Handlers[handler] = fn
	return true
}

func (j *AsyncTasksJob) Deregister(handler string) {
	j.mux.Lock()
	defer j.mux.Unlock()

	delete(j.Handlers, handler)
}

func (j *AsyncTasksJob) Interval() time.Duration {
	return 5 * time.Minute
}

func (j *AsyncTasksJob) Jitter() time.Duration {
	return 1 * time.Minute
}
func (j *AsyncTasksJob) Name() string {
	return "async_tasks_job"
}

func (j *AsyncTasksJob) NewParams() any {
	return &AsyncTasksParams{
		Count:        j.Count,
		PastInterval: j.PastInterval,
		MaxAttempts:  j.MaxAttempts,
	}
}

func (j *AsyncTasksJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*AsyncTasksParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*AsyncTasksParams)
	}

	// we want this context to be cancelled for other potential instance of the job to acquire the lock (before the DB query)
	handlerCtx, cancel := context.WithTimeout(ctx, j.Interval())
	defer cancel()

	tasks, err := j.BusinessDB.Impl().RetrievePendingAsyncTasks(ctx, p.Count, time.Now().UTC().Add(-p.PastInterval), p.MaxAttempts)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve pending tasks", common.ErrAttr(err))
		return err
	}

	for _, task := range tasks {
		if err := j.doExecute(handlerCtx, &task.AsyncTask); err != nil {
			slog.ErrorContext(ctx, "Failed to execute task handler", "handler", task.AsyncTask.Handler, "taskID", db.UUIDToString(task.AsyncTask.ID), common.ErrAttr(err))
		}
	}

	return nil
}

func (j *AsyncTasksJob) getHandlerSafe(id string) (db.AsyncTaskHandler, bool) {
	j.mux.Lock()
	defer j.mux.Unlock()

	handler, ok := j.Handlers[id]
	return handler, ok
}

func (j *AsyncTasksJob) Execute(ctx context.Context, task *dbgen.AsyncTask) error {
	select {
	case j.Semaphore <- struct{}{}:
		// release semaphore on exit
		defer func() { <-j.Semaphore }()
	default:
		// We strictly DO NOT want to wait. We skip this execution.
		// Returning nil means "no error", as the task remains in DB
		slog.InfoContext(ctx, "Skipping premature execution: concurrency limit reached", "taskID", db.UUIDToString(task.ID))
		return nil
	}

	return j.doExecute(ctx, task)
}

func (j *AsyncTasksJob) doExecute(ctx context.Context, task *dbgen.AsyncTask) error {
	if handler, ok := j.getHandlerSafe(task.Handler); ok {
		// Atomically claim the task before invoking the handler. The claim
		// increments processing_attempts and only succeeds while the task is
		// still pending (processed_at IS NULL) and below MaxAttempts. This
		// prevents concurrent runners (the immediate attempt goroutine and the
		// UniquePeriodicJob worker, or duplicate worker ticks) from executing
		// the same task more than MaxAttempts times.
		claimed, err := j.BusinessDB.Impl().ClaimAsyncTask(ctx, task.ID, j.MaxAttempts)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to claim async task", "taskID", db.UUIDToString(task.ID), common.ErrAttr(err))
			return err
		}
		if !claimed {
			// Another runner already claimed or completed this task, or it has
			// exhausted its attempts. Skip the handler to avoid duplicate
			// execution and duplicate side effects.
			slog.InfoContext(ctx, "Skipping async task: already claimed or completed", "taskID", db.UUIDToString(task.ID), "handler", task.Handler, "attempts", task.ProcessingAttempts)
			return nil
		}

		output, err := executeHandlerSafe(ctx, handler, task)
		var processedAt time.Time
		if err == nil {
			processedAt = time.Now().UTC()
		}
		// UpdateAsyncTask is guarded by processed_at IS NULL, so a late write
		// from a stale run cannot clobber a task that another runner already
		// completed. The processing_attempts increment was performed by the
		// claim above; this update only records the result.
		if updateErr := j.BusinessDB.Impl().UpdateAsyncTask(ctx, task.ID, output, processedAt); updateErr != nil {
			slog.ErrorContext(ctx, "Failed to update async task", "taskID", db.UUIDToString(task.ID), common.ErrAttr(updateErr))
		}

		return err
	}

	return errUnknownHandler
}

func executeHandlerSafe(ctx context.Context, handler db.AsyncTaskHandler, task *dbgen.AsyncTask) (output []byte, err error) {
	defer func() {
		if rvr := recover(); rvr != nil {
			slog.ErrorContext(ctx, "Async task handler crashed", "handler", task.Handler, "panic", rvr, "stack", string(debug.Stack()))
			output = nil
			err = errHandlerCrashed
		}
	}()

	return handler(ctx, task)
}
