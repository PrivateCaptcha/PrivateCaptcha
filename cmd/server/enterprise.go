//go:build enterprise

package main

import (
	"context"
	"errors"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func checkLicense(context.Context, common.ConfigStore) error {
	return errors.New("enterprise version requires a license (https://privatecaptcha.com/)")
}
