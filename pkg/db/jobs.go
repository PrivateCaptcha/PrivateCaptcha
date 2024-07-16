package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type UniquePeriodicJob struct {
	Job   common.PeriodicJob
	Store *BusinessStore
}

var _ common.PeriodicJob = (*UniquePeriodicJob)(nil)

func (j *UniquePeriodicJob) Interval() time.Duration {
	return j.Job.Interval()
}

func (j *UniquePeriodicJob) Jitter() time.Duration {
	return j.Job.Jitter()
}

func (j *UniquePeriodicJob) Name() string {
	return j.Job.Name()
}

func (j *UniquePeriodicJob) RunOnce(ctx context.Context) error {
	var jerr error
	lockName := j.Job.Name()
	expiration := time.Now().UTC().Add(j.Job.Interval())
	// we always acquire the lock for {interval} duration so after (interval + jitter) it will definitely be free
	if _, err := j.Store.AcquireLock(ctx, lockName, nil /*data*/, expiration); err == nil {
		jerr = j.RunOnce(ctx)
		if jerr != nil {
			// NOTE: in usual circumstances we do not release the lock, letting it expire by TTL, thus effectively
			// preventing other possible maintenance jobs during the interval. The only use-case is when the job
			// itself fails, then we want somebody to retry "sooner"
			j.Store.ReleaseLock(ctx, lockName)
		}
	} else {
		level := slog.LevelError
		if err == ErrLocked {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "Failed to acquire a lock for periodic job", "name", lockName, common.ErrAttr(err))
	}

	return jerr
}
