package maintenance

import (
	"context"
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

func (j *CleanupDBCacheJob) Name() string {
	return "cleanup_db_cache_job"
}

func (j *CleanupDBCacheJob) RunOnce(ctx context.Context) error {
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

func (j *CleanupDeletedRecordsJob) Name() string {
	return "cleanup_deleted_records_job"
}

func (j *CleanupDeletedRecordsJob) RunOnce(ctx context.Context) error {
	before := time.Now().UTC().Add(-j.Age)
	return j.Store.Impl().DeleteDeletedRecords(ctx, before)
}
