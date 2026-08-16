//go:build !enterprise

package maintenance

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
)

func NewBrowserVersionJobs(db.Implementor, *rules.BrowserVersions) (common.PeriodicJob, common.PeriodicJob) {
	return nil, nil
}
