package maintenance

import (
	"context"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

const (
	maxSoftDeletedProperties    = 30
	maxSoftDeletedOrganizations = 30
	maxSoftDeletedUsers         = 30
)

type GarbageCollectDataJob struct {
	Age        time.Duration
	BusinessDB db.Implementor
	TimeSeries common.TimeSeriesStore
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
	if properties, err := j.BusinessDB.Impl().RetrieveSoftDeletedProperties(ctx, before, maxSoftDeletedProperties); (err == nil) && (len(properties) > 0) {
		ids := make([]int32, 0, len(properties))
		for _, p := range properties {
			ids = append(ids, p.Property.ID)
		}

		if err := j.TimeSeries.DeletePropertiesData(ctx, ids); err == nil {
			_ = j.BusinessDB.Impl().DeleteProperties(ctx, ids)
		}
	}

	return nil
}

func (j *GarbageCollectDataJob) purgeOrganizations(ctx context.Context, before time.Time) error {
	// NOTE: we're processing organizations that are soft-deleted, but user is not
	if organizations, err := j.BusinessDB.Impl().RetrieveSoftDeletedOrganizations(ctx, before, maxSoftDeletedOrganizations); (err == nil) && (len(organizations) > 0) {
		ids := make([]int32, 0, len(organizations))
		for _, p := range organizations {
			ids = append(ids, p.Organization.ID)
		}

		if err := j.TimeSeries.DeleteOrganizationsData(ctx, ids); err == nil {
			_ = j.BusinessDB.Impl().DeleteOrganizations(ctx, ids)
		}
	}

	return nil

}

func (j *GarbageCollectDataJob) purgeUsers(ctx context.Context, before time.Time) error {
	if users, err := j.BusinessDB.Impl().RetrieveSoftDeletedUsers(ctx, before, maxSoftDeletedUsers); (err == nil) && (len(users) > 0) {
		ids := make([]int32, 0, len(users))
		for _, p := range users {
			ids = append(ids, p.User.ID)
		}

		if err := j.TimeSeries.DeleteUsersData(ctx, ids); err == nil {
			_ = j.BusinessDB.Impl().DeleteUsers(ctx, ids)
		}
	}

	return nil

}

func (j *GarbageCollectDataJob) RunOnce(ctx context.Context) error {
	before := time.Now().UTC().Add(-j.Age)
	if err := j.purgeProperties(ctx, before); err != nil {
		return err
	}

	if err := j.purgeOrganizations(ctx, before); err != nil {
		return err
	}

	if err := j.purgeUsers(ctx, before); err != nil {
		return err
	}

	return nil
}
