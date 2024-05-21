package portal

import (
	"context"
	"log/slog"
)

type Mailer interface {
	SendTwoFactor(ctx context.Context, email string, code int) error
	SendSupportRequest(ctx context.Context, email string, category string, message string) error
}

// TODO: Implement actual SMTP mailer
type StubMailer struct {
	lastCode  int
	lastEmail string
}

var _ Mailer = (*StubMailer)(nil)

func (sm *StubMailer) SendTwoFactor(ctx context.Context, email string, code int) error {
	slog.InfoContext(ctx, "Sent two factor code via email", "code", code, "email", email)
	sm.lastCode = code
	sm.lastEmail = email
	return nil
}

func (sm *StubMailer) SendSupportRequest(ctx context.Context, email string, category string, message string) error {
	slog.InfoContext(ctx, "Sent support request", "category", category, "email", email)
	return nil
}
