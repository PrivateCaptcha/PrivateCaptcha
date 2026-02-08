//go:build enterprise

package maintenance

import (
	"context"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func licenseConfigKey() common.ConfigKey {
	return common.EnterpriseLicenseKeyKey
}

func requireActivationKeys() bool {
	return true
}

func (j *CheckLicenseJob) RunOnce(ctx context.Context, params any) error {
	if err := j.checkLicense(ctx); err != nil {
		j.licenseValid.Store(false)
		go j.quitFunc(common.CopyTraceID(ctx, context.Background()))
		return err
	}

	j.licenseValid.Store(true)

	slog.DebugContext(ctx, "License check passed")
	return nil
}
