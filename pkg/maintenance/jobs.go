package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"golang.org/x/sync/semaphore"

	"github.com/justinas/alice"
)

func NewJobs(store db.Implementor, concurrency int) *jobs {
	if concurrency < 1 {
		concurrency = 1
	}
	j := &jobs{
		store:        store,
		periodicJobs: make([]common.PeriodicJob, 0),
		oneOffJobs:   make([]common.OneOffJob, 0),
		sem:          semaphore.NewWeighted(int64(concurrency)),
	}

	j.maintenanceCtx, j.maintenanceCancel = context.WithCancel(
		context.WithValue(context.Background(), common.ServiceContextKey, "maintenance"))

	return j
}

type jobs struct {
	store             db.Implementor
	periodicJobs      []common.PeriodicJob
	oneOffJobs        []common.OneOffJob
	maintenanceCancel context.CancelFunc
	maintenanceCtx    context.Context
	sem               *semaphore.Weighted
}

// Implicit logic is that lockDuration is the actual job Interval, but it is defined by the SQL lock.
// Job's Interval() is much smaller only for the purpose of "retrying" if the previous job execution failed
func (j *jobs) AddLocked(lockDuration time.Duration, job common.PeriodicJob) {
	if job == nil {
		return
	}

	if interval := job.Interval(); interval >= lockDuration {
		slog.Error("Periodic job interval should be less than lock duration", "job", job.Name(), "lock", lockDuration.String(), "interval", interval.String())
	}

	j.periodicJobs = append(j.periodicJobs, &UniquePeriodicJob{
		Job:          job,
		Store:        j.store,
		LockDuration: lockDuration,
	})
}

func (j *jobs) Add(job common.PeriodicJob) {
	if job == nil {
		return
	}

	j.periodicJobs = append(j.periodicJobs, job)
}

func (j *jobs) AddOneOff(job common.OneOffJob) {
	if job == nil {
		return
	}

	j.oneOffJobs = append(j.oneOffJobs, job)
}

// spawned jobs only share common cancellation context and are not exclusive
func (j *jobs) Spawn(job common.PeriodicJob) {
	if job == nil {
		return
	}

	go common.RunPeriodicJob(j.maintenanceCtx, job)
}

func (j *jobs) RunAll() {
	slog.DebugContext(j.maintenanceCtx, "Starting maintenance jobs", "periodic", len(j.periodicJobs), "oneoff", len(j.oneOffJobs))

	// NOTE: we limit concurrent jobs with semaphore to preserve resources for main server (those are _maintenance_ jobs anyways)
	// NOTE 2: this does not apply for on-demand ones below - that's why we wrap them only here, unlike AddLocked()

	for _, job := range j.periodicJobs {
		go common.RunPeriodicJob(j.maintenanceCtx, &semaphorePeriodicJob{job: job, sem: j.sem})
	}

	for _, job := range j.oneOffJobs {
		go common.RunOneOffJob(j.maintenanceCtx, &semaphoreOneOffJob{job: job, sem: j.sem}, job.NewParams())
	}
}

func (j *jobs) Setup(mux *http.ServeMux, middleware alice.Chain) {
	const maxBytes = 256 * 1024
	mux.Handle(http.MethodPost+" /maintenance/periodic/{job}", middleware.Then(http.MaxBytesHandler(http.HandlerFunc(j.handlePeriodicJob), maxBytes)))
	mux.Handle(http.MethodPost+" /maintenance/oneoff/{job}", middleware.Then(http.MaxBytesHandler(http.HandlerFunc(j.handleOneoffJob), maxBytes)))
}

func (j *jobs) handlePeriodicJob(w http.ResponseWriter, r *http.Request) {
	jobName, err := common.StrPathArg(r, "job")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	slog.InfoContext(ctx, "Handling on-demand periodic job launch", "job", jobName)
	found := false

	for _, job := range j.periodicJobs {
		if job.Name() == jobName {
			params := job.NewParams()
			if r.Body != nil {
				if buf, _ := io.ReadAll(r.Body); len(buf) > 0 {
					if err := json.Unmarshal(buf, params); err != nil {
						slog.ErrorContext(ctx, "Failed to decode params", "job", jobName, common.ErrAttr(err))
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					} else {
						slog.DebugContext(ctx, "Read job parameters from request", "size", len(buf))
					}
				}
			}

			go func() {
				_ = common.RunPeriodicJobOnce(common.CopyTraceID(ctx, j.maintenanceCtx), job, params, 1*time.Second)
			}()
			found = true
			break
		}
	}

	if !found {
		http.Error(w, fmt.Sprintf("job %v not found", jobName), http.StatusBadRequest)
		return
	}

	_, _ = w.Write([]byte("started"))
}

func (j *jobs) handleOneoffJob(w http.ResponseWriter, r *http.Request) {
	jobName, err := common.StrPathArg(r, "job")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	slog.InfoContext(ctx, "Handling on-demand one-off job launch", "job", jobName)
	found := false

	for _, job := range j.oneOffJobs {
		if job.Name() == jobName {
			params := job.NewParams()
			if r.Body != nil {
				if buf, _ := io.ReadAll(r.Body); len(buf) > 0 {
					if err := json.Unmarshal(buf, params); err != nil {
						slog.ErrorContext(ctx, "Failed to decode params", "job", jobName, common.ErrAttr(err))
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
				}
			}

			go common.RunOneOffJob(common.CopyTraceID(ctx, j.maintenanceCtx), job, params)

			found = true
			break
		}
	}

	if !found {
		http.Error(w, fmt.Sprintf("job %v not found", jobName), http.StatusBadRequest)
		return
	}

	_, _ = w.Write([]byte("started"))
}

func (j *jobs) Stop() {
	slog.Debug("Shutting down maintenance jobs")

	if j.maintenanceCancel != nil {
		j.maintenanceCancel()
	}
}
