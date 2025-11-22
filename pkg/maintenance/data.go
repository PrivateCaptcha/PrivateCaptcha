package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/jpillora/backoff"
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

type GarbageCollectDataParams struct {
	Age time.Duration `json:"age"`
}

func (j *GarbageCollectDataJob) NewParams() any {
	return &GarbageCollectDataParams{
		Age: j.Age,
	}
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

func (j *GarbageCollectDataJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*GarbageCollectDataParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*GarbageCollectDataParams)
	}

	before := time.Now().UTC().Add(-p.Age)
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

type CleanupExpiredTrialUsersJob struct {
	Age         time.Duration
	BusinessDB  db.Implementor
	PlanService billing.PlanService
	ChunkSize   int
	Months      int
}

var _ common.PeriodicJob = (*CleanupExpiredTrialUsersJob)(nil)

func (j *CleanupExpiredTrialUsersJob) Interval() time.Duration {
	return 12 * time.Hour
}

func (j *CleanupExpiredTrialUsersJob) Jitter() time.Duration {
	return 6 * time.Hour
}

func (j *CleanupExpiredTrialUsersJob) Name() string {
	return "cleanup_expired_trial_users_job"
}

type CleanupExpiredTrialUsersParams struct {
	Age       time.Duration `json:"age"`
	ChunkSize int           `json:"chunk_size"`
	Months    int           `json:"months"`
}

func (j *CleanupExpiredTrialUsersJob) NewParams() any {
	return &CleanupExpiredTrialUsersParams{
		Age:       j.Age,
		ChunkSize: j.ChunkSize,
		Months:    j.Months,
	}
}

func (j *CleanupExpiredTrialUsersJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*CleanupExpiredTrialUsersParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*CleanupExpiredTrialUsersParams)
	}

	expiredTo := time.Now().Add(-p.Age)
	expiredFrom := expiredTo.AddDate(0, -p.Months, 0)
	users, err := j.BusinessDB.Impl().RetrieveTrialUsers(ctx, expiredFrom, expiredTo, j.PlanService.ExpiredTrialStatus(), int32(p.ChunkSize), true /*internal*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve users with expired trials", common.ErrAttr(err))
		return err
	}

	b := &backoff.Backoff{
		Min:    200 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	for i, u := range users {
		if (i > 0) && (err == nil) {
			time.Sleep(b.Duration())
		}

		// NOTE: we don't record audit event because this is not what user did themselves
		_, err = j.BusinessDB.Impl().SoftDeleteUser(ctx, u)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to soft-delete user", "userID", u.ID, common.ErrAttr(err))
		}
	}

	return nil
}

type ExpireInternalTrialsJob struct {
	PastInterval time.Duration
	Age          time.Duration
	BusinessDB   db.Implementor
	PlanService  billing.PlanService
}

var _ common.PeriodicJob = (*ExpireInternalTrialsJob)(nil)

func (ExpireInternalTrialsJob) Interval() time.Duration {
	return 1 * time.Hour
}

func (j *ExpireInternalTrialsJob) Jitter() time.Duration {
	return 30 * time.Minute
}

func (j *ExpireInternalTrialsJob) Name() string {
	return "expire_internal_trials_job"
}

type ExpireInternalTrialsParams struct {
	PastInterval time.Duration `json:"past_interval"`
	Age          time.Duration `json:"age"`
}

func (j *ExpireInternalTrialsJob) NewParams() any {
	return &ExpireInternalTrialsParams{
		PastInterval: j.PastInterval,
		Age:          j.Age,
	}
}

func (j *ExpireInternalTrialsJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*ExpireInternalTrialsParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*ExpireInternalTrialsParams)
	}

	to := time.Now().Add(-p.Age)
	from := to.Add(-(p.PastInterval + j.Interval() + j.Jitter()))
	return j.BusinessDB.Impl().ExpireInternalTrials(ctx, from, to, j.PlanService.ActiveTrialStatus(), j.PlanService.ExpiredTrialStatus())
}

type CleanupAuditLogJob struct {
	BusinessDB   db.Implementor
	PastInterval time.Duration
}

type CleanupAuditLogParams struct {
	PastInterval time.Duration `json:"past_interval"`
}

func (j *CleanupAuditLogJob) NewParams() any {
	return &CleanupAuditLogParams{
		PastInterval: j.PastInterval,
	}
}
func (j *CleanupAuditLogJob) RunOnce(ctx context.Context, params any) error {
	p, ok := params.(*CleanupAuditLogParams)
	if !ok || (p == nil) {
		slog.ErrorContext(ctx, "Job parameter has incorrect type", "params", params, "job", j.Name())
		p = j.NewParams().(*CleanupAuditLogParams)
	}

	return j.BusinessDB.Impl().DeleteOldAuditLogs(ctx, time.Now().UTC().Add(-p.PastInterval))
}

func (j *CleanupAuditLogJob) Interval() time.Duration {
	return 1 * time.Hour
}

func (j *CleanupAuditLogJob) Jitter() time.Duration {
	return 30 * time.Minute
}

func (j *CleanupAuditLogJob) Name() string {
	return "cleanup_audit_log_job"
}
