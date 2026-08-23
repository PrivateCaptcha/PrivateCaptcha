package maintenance

import (
	"context"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

const (
	maxPaginationIterations        = 100
	topPropertiesLimit             = 5
	weeklySecurityEventsLimit      = 3
	monthlySecurityEventsLimit     = 5
	securityEventsPerProperty      = 2
	protectionRatioThreshold       = 3.0
	protectionMinimumDominantCount = 100
	floatEpsilon                   = 1e-4

	WeeklyReferencePrefix  = "report/weekly/"
	MonthlyReferencePrefix = "report/monthly/"
)

func userReportOptions(protectionHighlightsLimit int) common.UserReportOptions {
	return common.UserReportOptions{
		TopPropertiesLimit:                topPropertiesLimit,
		SecurityEventsLimit:               protectionHighlightsLimit,
		SecurityEventsPerPropertyLimit:    securityEventsPerProperty,
		SecurityEventRatioThreshold:       protectionRatioThreshold,
		SecurityEventMinimumDominantCount: protectionMinimumDominantCount,
	}
}

type ScheduleReportsJob struct {
	Store       db.Implementor
	TimeSeries  common.TimeSeriesStore
	PlanService billing.PlanService
	IDHasher    common.IdentifierHasher
	PortalURL   string
	Stage       string
	UsersLimit  int32
}

type ScheduleReportsParams struct {
	UsersLimit int32  `json:"users_limit,omitempty"`
	UserID     int32  `json:"user_id,omitempty"`
	UserEmail  string `json:"user_email,omitempty"`
	Weekly     bool   `json:"weekly,omitempty"`
	Monthly    bool   `json:"monthly,omitempty"`
}

var _ common.PeriodicJob = (*ScheduleReportsJob)(nil)

func (j *ScheduleReportsJob) Name() string {
	return "schedule_reports_job"
}

func (j *ScheduleReportsJob) Interval() time.Duration {
	return 2 * time.Hour
}

func (j *ScheduleReportsJob) Timeout() time.Duration {
	return 5 * time.Minute
}

func (j *ScheduleReportsJob) Jitter() time.Duration {
	return 30 * time.Minute
}

func (j *ScheduleReportsJob) Trigger() <-chan struct{} {
	return nil
}

func (j *ScheduleReportsJob) NewParams() any {
	limit := j.UsersLimit
	if limit <= 0 {
		limit = 50
	}
	return &ScheduleReportsParams{
		UsersLimit: limit,
		Weekly:     true,
		Monthly:    true,
	}
}

func (j *ScheduleReportsJob) RunOnce(ctx context.Context, params any) error {
	return j.RunOnceAt(ctx, params, time.Now().UTC())
}
