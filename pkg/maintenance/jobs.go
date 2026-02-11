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
)

func NewJobs(store db.Implementor, concurrency int, metrics common.BaseMetrics) *jobs {
	if concurrency < 1 {
		concurrency = 1
	}
	j := &jobs{
		store:        store,
		periodicJobs: make([]common.PeriodicJob, 0),
		oneOffJobs:   make([]common.OneOffJob, 0),
		sem:          semaphore.NewWeighted(int64(concurrency)),
		metrics:      metrics,
	}

	j.maintenanceCtx, j.maintenanceCancel = context.WithCancel(
		context.WithValue(context.Background(), common.ServiceContextKey, "maintenance"))

	return j
}

type jobs struct {
	store             db.Implementor
	metrics           common.BaseMetrics
	periodicJobs      []common.PeriodicJob
	oneOffJobs        []common.OneOffJob
	maintenanceCancel context.CancelFunc
	maintenanceCtx    context.Context
	apiKey            string
	sem               *semaphore.Weighted
}

// Implicit logic is that lockDuration is the actual job Interval, but it is defined by the SQL lock.
// Job's Interval() is much smaller only for the purpose of "retrying" if the previous job execution failed
func (j *jobs) AddLocked(lockDuration time.Duration, job common.PeriodicJob) {
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
	j.periodicJobs = append(j.periodicJobs, job)
}

func (j *jobs) AddOneOff(job common.OneOffJob) {
	j.oneOffJobs = append(j.oneOffJobs, job)
}

// spawned jobs only share common cancellation context and are not exclusive
func (j *jobs) Spawn(job common.PeriodicJob) {
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

func (j *jobs) UpdateConfig(cfg common.ConfigStore) {
	j.apiKey = cfg.Get(common.LocalAPIKeyKey).Value()
}

func (j *jobs) Setup(mux *http.ServeMux, cfg common.ConfigStore) {
	j.apiKey = cfg.Get(common.LocalAPIKeyKey).Value()

	svc := common.ServiceMiddleware("local")
	recovered := common.Recovered(j.metrics)

	const maxBytes = 256 * 1024
	mux.Handle(http.MethodPost+" /maintenance/periodic/{job}", svc(recovered(http.MaxBytesHandler(j.security(http.HandlerFunc(j.handlePeriodicJob)), maxBytes))))
	mux.Handle(http.MethodPost+" /maintenance/oneoff/{job}", svc(recovered(http.MaxBytesHandler(j.security(http.HandlerFunc(j.handleOneoffJob)), maxBytes))))
}

func (j *jobs) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if len(j.apiKey) == 0 {
			slog.WarnContext(ctx, "Endpoint is not allowed without a configured API key")
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		secret := r.Header.Get(common.HeaderAPIKey)
		if len(secret) == 0 {
			slog.WarnContext(ctx, "Request API key is empty")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		if secret != j.apiKey {
			slog.WarnContext(ctx, "Request API key does not match", "value", secret)
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
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
					}
				}
			}

			go func() {
				_ = common.RunPeriodicJobOnce(common.CopyTraceID(ctx, context.Background()), job, params)
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

			go common.RunOneOffJob(common.CopyTraceID(ctx, context.Background()), job, params)

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
