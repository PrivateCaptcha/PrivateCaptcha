//go:build !enterprise

package main

import (
	"context"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func checkLicense(context.Context, common.ConfigStore) error {
	// not implemented
	return nil
}
