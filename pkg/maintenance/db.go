package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type CleanupDBCacheJob struct {
	Store db.Implementor
}

var _ common.PeriodicJob = (*CleanupDBCacheJob)(nil)

func (j *CleanupDBCacheJob) Interval() time.Duration {
	return 5 * time.Minute
}

func (j *CleanupDBCacheJob) Jitter() time.Duration {
	return 1
}

func (j *CleanupDBCacheJob) Trigger() <-chan struct{} {
	return nil
}

func (j *CleanupDBCacheJob) Name() string {
	return "cleanup_db_cache_job"
}

func (j *CleanupDBCacheJob) NewParams() any {
	return struct{}{}
}

func (j *CleanupDBCacheJob) RunOnce(ctx context.Context, params any) error {
	return j.Store.Impl().DeleteExpiredCache(ctx)
}

type CleanupDeletedRecordsJob struct {
	Store db.Implementor
	Age   time.Duration
}

var _ common.PeriodicJob = (*CleanupDeletedRecordsJob)(nil)

func (j *CleanupDeletedRecordsJob) Interval() time.Duration {
	return 24 * time.Hour
}

func (j *CleanupDeletedRecordsJob) Jitter() time.Duration {
	return 1
}

func (j *CleanupDeletedRecordsJob) Trigger() <-chan struct{} {
	return nil
}

func (j *CleanupDeletedRecordsJob) Name() string {
	return "cleanup_deleted_records_job"
}

type CleanupDeletedRecordsParams struct {
	Age time.Duration `json:"age"`
}

func (j *CleanupDeletedRecordsJob) NewParams() any {
	return &CleanupDeletedRecordsParams{
		Age: j.Age,
	}
}

func (j *CleanupDeletedRecordsJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*CleanupDeletedRecordsParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*CleanupDeletedRecordsParams)
	}

	before := time.Now().UTC().Add(-p.Age)
	return j.Store.Impl().DeleteDeletedRecords(ctx, before)
}
