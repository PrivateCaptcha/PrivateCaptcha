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

type DeleteSoftDeletedDataJob struct {
	Since      time.Duration
	BusinessDB *db.BusinessStore
	TimeSeries *db.TimeSeriesStore
}

var _ common.PeriodicJob = (*DeleteSoftDeletedDataJob)(nil)

func (j *DeleteSoftDeletedDataJob) Interval() time.Duration {
	return 1 * time.Hour
}

func (j *DeleteSoftDeletedDataJob) Jitter() time.Duration {
	return 1 * time.Hour
}

func (j *DeleteSoftDeletedDataJob) Name() string {
	return "delete_soft_deleted_data_job"
}

func (j *DeleteSoftDeletedDataJob) purgeProperties(ctx context.Context, since time.Time) error {
	// NOTE: we're processing properties that are soft-deleted, but org is not
	if properties, err := j.BusinessDB.RetrieveSoftDeletedProperties(ctx, since, maxSoftDeletedProperties); (err == nil) && (len(properties) > 0) {
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

func (j *DeleteSoftDeletedDataJob) RunOnce(ctx context.Context) error {
	return j.purgeProperties(ctx, time.Now().UTC().Add(-j.Since))
}
