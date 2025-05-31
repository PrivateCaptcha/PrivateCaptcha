package email

import (
	"context"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type StubMailer struct {
	LastCode  int
	LastEmail string
}

var _ common.Mailer = (*StubMailer)(nil)

func (sm *StubMailer) SendTwoFactor(ctx context.Context, email string, code int) error {
	slog.InfoContext(ctx, "Sent two factor code via email", "code", code, "email", email)
	sm.LastCode = code
	sm.LastEmail = email
	return nil
}

func (sm *StubMailer) SendWelcome(ctx context.Context, email string) error {
	slog.InfoContext(ctx, "Sent welcome email", "email", email)
	return nil
}
