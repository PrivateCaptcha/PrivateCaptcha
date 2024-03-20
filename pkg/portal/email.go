package portal

import (
	"context"
	"log/slog"
)

type Mailer interface {
	SendTwoFactor(ctx context.Context, email string, code int) error
}

// TODO: Implement actual SMTP mailer
type StubMailer struct{}

var _ Mailer = (*StubMailer)(nil)

func (sm *StubMailer) SendTwoFactor(ctx context.Context, email string, code int) error {
	slog.InfoContext(ctx, "Sent two factor code via email", "code", code, "email", email)
	return nil
}
