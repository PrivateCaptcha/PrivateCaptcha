package maintenance

import (
	"context"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type CleanupDBCacheJob struct {
	Store *db.BusinessStore
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
	return j.Store.DeleteExpiredCache(ctx)
}
