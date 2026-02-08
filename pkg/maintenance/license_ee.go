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

func (j *checkLicenseJob) RunOnce(ctx context.Context, params any) error {
	if err := j.checkLicense(ctx); err != nil {
		go j.quitFunc(ctx)
		return err
	}

	if j.licenseValid != nil {
		j.licenseValid.Store(true)
	}

	slog.DebugContext(ctx, "License check passed")
	return nil
}
