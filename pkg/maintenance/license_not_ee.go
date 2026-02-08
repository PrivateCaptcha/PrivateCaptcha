//go:build !enterprise

package maintenance

import (
	"context"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func licenseConfigKey() common.ConfigKey {
	return common.CommunityLicenseKeyKey
}

func requireActivationKeys() bool {
	return false
}

func (j *checkLicenseJob) RunOnce(ctx context.Context, params any) error {
	if err := j.checkLicense(ctx); err != nil {
		slog.WarnContext(ctx, "License check failed", common.ErrAttr(err))
		if j.licenseValid != nil {
			j.licenseValid.Store(false)
		}
		return nil
	}

	if j.licenseValid != nil {
		j.licenseValid.Store(true)
	}

	slog.DebugContext(ctx, "License check passed")
	return nil
}
