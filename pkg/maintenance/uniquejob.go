package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type UniquePeriodicJob struct {
	Job   common.PeriodicJob
	Store db.Implementor
	// the usual logic is that we acquire lock for a longer duration than the job interval therefore
	// when there are multiple workers, there's a higher chance of "stealing" the work
	LockDuration time.Duration
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

func (j *UniquePeriodicJob) NewParams() any {
	return j.Job.NewParams()
}

func (j *UniquePeriodicJob) acquireLock(ctx context.Context, lockName string) error {
	expiration := time.Now().UTC().Add(j.LockDuration)

	_, err := j.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		_, err := impl.AcquireLock(ctx, lockName, nil /*data*/, expiration)
		return nil, err
	})
	return err
}

func (j *UniquePeriodicJob) releaseLock(ctx context.Context, lockName string) error {
	_, err := j.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		err := impl.ReleaseLock(ctx, lockName)
		return nil, err
	})

	return err
}

func (j *UniquePeriodicJob) RunOnce(ctx context.Context, params any) error {
	var jerr error
	lockName := j.Job.Name()

	// TODO: Acquire the lock incrementally instead of the full duration
	// this will help to handle situations when we crash and don't release the lock
	if err := j.acquireLock(ctx, lockName); err == nil {
		jerr = j.Job.RunOnce(ctx, params)
		if jerr != nil {
			// NOTE: in usual circumstances we do NOT release the lock, letting it expire by TTL, thus effectively
			// preventing other possible maintenance jobs during the interval. The only use-case is when the job
			// itself fails, then we want somebody to retry "sooner"
			if rerr := j.releaseLock(ctx, lockName); rerr != nil {
				slog.ErrorContext(ctx, "Failed to release the lock for periodic job", "name", lockName, common.ErrAttr(rerr))
			}
		}
	} else {
		level := slog.LevelError
		if err == db.ErrLocked {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "Failed to acquire a lock for periodic job", "name", lockName, common.ErrAttr(err))
	}

	return jerr
}
