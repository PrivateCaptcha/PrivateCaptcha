//go:build !enterprise

package maintenance

import (
	"context"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

func NewCheckLicenseJob(db.Implementor, common.ConfigStore, string, func(ctx context.Context)) (common.PeriodicJob, error) {
	return &checkLicenseNoopJob{}, nil
}

type checkLicenseNoopJob struct {
}

func (j *checkLicenseNoopJob) Trigger() <-chan struct{} { return nil }

func (j *checkLicenseNoopJob) RunOnce(ctx context.Context, params any) error {
	return nil
}

func (j *checkLicenseNoopJob) NewParams() any {
	return struct{}{}
}

func (j *checkLicenseNoopJob) Interval() time.Duration {
	return 1 * time.Hour
}

func (j *checkLicenseNoopJob) Jitter() time.Duration {
	return 1
}

func (j *checkLicenseNoopJob) Name() string {
	return "check_license_noop_job"
}
