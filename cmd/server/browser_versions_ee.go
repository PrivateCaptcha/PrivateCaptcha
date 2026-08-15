//go:build enterprise

package main

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
)

func newBrowserVersionJobs(store db.Implementor, compiler *rules.RulesCompiler) (common.PeriodicJob, common.PeriodicJob) {
	fetchTrigger := make(chan struct{}, 1)
	refreshTrigger := make(chan struct{}, 1)
	fetchJob := maintenance.NewFetchBrowserVersionsJob(store)
	fetchJob.TriggerCh = fetchTrigger
	fetchJob.RefreshTrigger = refreshTrigger
	refreshJob := maintenance.NewRefreshBrowserVersionsJob(store, compiler.BrowserVersions())
	refreshJob.TriggerCh = refreshTrigger

	fetchTrigger <- struct{}{}
	refreshTrigger <- struct{}{}
	return fetchJob, refreshJob
}
