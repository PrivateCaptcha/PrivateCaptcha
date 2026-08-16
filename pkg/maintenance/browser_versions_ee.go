//go:build enterprise

package maintenance

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
)

func NewBrowserVersionJobs(store db.Implementor, versions *rules.BrowserVersions) (common.PeriodicJob, common.PeriodicJob) {
	fetchJob := NewFetchBrowserVersionsJob(store)
	refreshJob := NewRefreshBrowserVersionsJob(store, versions)
	fetchJob.RefreshTrigger = refreshJob.TriggerCh

	return fetchJob, refreshJob
}
