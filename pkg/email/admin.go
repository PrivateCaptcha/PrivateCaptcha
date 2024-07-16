package email

import (
	"context"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type StubAdminMailer struct {
}

var _ common.AdminMailer = (*StubAdminMailer)(nil)

func (am *StubAdminMailer) SendUsageViolations(ctx context.Context, emails []string) error {
	slog.InfoContext(ctx, "Sent usage violations", "count", len(emails))
	return nil
}
