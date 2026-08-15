//go:build !enterprise

package main

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
)

func newBrowserVersionJobs(db.Implementor, *rules.RulesCompiler) (common.PeriodicJob, common.PeriodicJob) {
	return common.StubPeriodicJob("fetch_browser_versions_job"), common.StubPeriodicJob("refresh_browser_versions_job")
}
