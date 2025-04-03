package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

func NewJobs(store *db.BusinessStore) *jobs {
	return &jobs{
		store:        store,
		periodicJobs: make([]common.PeriodicJob, 0),
		oneOffJobs:   make([]common.OneOffJob, 0),
	}
}

type jobs struct {
	store             *db.BusinessStore
	periodicJobs      []common.PeriodicJob
	oneOffJobs        []common.OneOffJob
	maintenanceCancel context.CancelFunc
	maintenanceCtx    context.Context
}

func (j *jobs) AddLocked(lockDuration time.Duration, job common.PeriodicJob) {
	j.periodicJobs = append(j.periodicJobs, &UniquePeriodicJob{
		Job:          job,
		Store:        j.store,
		LockDuration: lockDuration,
	})
}

func (j *jobs) Add(job common.PeriodicJob) {
	j.periodicJobs = append(j.periodicJobs, job)
}

func (j *jobs) AddOneOff(job common.OneOffJob) {
	j.oneOffJobs = append(j.oneOffJobs, job)
}

func (j *jobs) Run() {
	j.maintenanceCtx, j.maintenanceCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "maintenance"))

	slog.DebugContext(j.maintenanceCtx, "Starting maintenance jobs", "periodic", len(j.periodicJobs), "oneoff", len(j.oneOffJobs))

	for _, job := range j.periodicJobs {
		go common.RunPeriodicJob(j.maintenanceCtx, job)
	}

	for _, job := range j.oneOffJobs {
		go common.RunOneOffJob(j.maintenanceCtx, job)
	}
}

func (j *jobs) Setup(mux *http.ServeMux) {
	mux.HandleFunc(http.MethodPost+" /maintenance/periodic/{job}", j.handlePeriodicJob)
	mux.HandleFunc(http.MethodPost+" /maintenance/oneoff/{job}", j.handleOneoffJob)
}

func (j *jobs) handlePeriodicJob(w http.ResponseWriter, r *http.Request) {
	jobName, err := common.StrPathArg(r, "job")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	slog.DebugContext(ctx, "Handling on-demand periodic job launch", "job", jobName)
	found := false

	for _, job := range j.periodicJobs {
		if job.Name() == jobName {
			go common.RunOncePeriodicJob(common.CopyTraceID(ctx, context.Background()), job)
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
	slog.DebugContext(ctx, "Handling on-demand one-off job launch", "job", jobName)
	found := false

	for _, job := range j.oneOffJobs {
		if job.Name() == jobName {
			go common.RunOneOffJob(common.CopyTraceID(ctx, context.Background()), job)
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

func (j *jobs) Shutdown() {
	slog.Debug("Shutting down maintenance jobs")

	if j.maintenanceCancel != nil {
		j.maintenanceCancel()
	}
}
