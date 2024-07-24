package maintenance

import (
	"context"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

const (
	maxSoftDeletedProperties = 30
)

type GarbageCollectDataJob struct {
	Age        time.Duration
	BusinessDB *db.BusinessStore
	TimeSeries *db.TimeSeriesStore
}

var _ common.PeriodicJob = (*GarbageCollectDataJob)(nil)

func (j *GarbageCollectDataJob) Interval() time.Duration {
	return 1 * time.Hour
}

func (j *GarbageCollectDataJob) Jitter() time.Duration {
	return 1 * time.Hour
}

func (j *GarbageCollectDataJob) Name() string {
	return "garbage_collect_data_job"
}

func (j *GarbageCollectDataJob) purgeProperties(ctx context.Context, before time.Time) error {
	// NOTE: we're processing properties that are soft-deleted, but org is not
	if properties, err := j.BusinessDB.RetrieveSoftDeletedProperties(ctx, before, maxSoftDeletedProperties); (err == nil) && (len(properties) > 0) {
		ids := make([]int32, 0, len(properties))
		for _, p := range properties {
			ids = append(ids, p.Property.ID)
		}

		if err := j.TimeSeries.DeletePropertiesData(ctx, ids); err == nil {
			_ = j.BusinessDB.DeleteProperties(ctx, ids)
		}
	}

	return nil

}

func (j *GarbageCollectDataJob) RunOnce(ctx context.Context) error {
	before := time.Now().UTC().Add(-j.Age)
	return j.purgeProperties(ctx, before)
}
