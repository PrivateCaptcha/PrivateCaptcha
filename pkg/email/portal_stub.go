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

func (sm *StubMailer) SendTwoFactor(ctx context.Context, email string, code int, ua string, location string) error {
	slog.InfoContext(ctx, "Sent two factor code via email", "code", code, "email", email)
	sm.LastCode = code
	sm.LastEmail = email
	return nil
}

func (sm *StubMailer) SendWelcome(ctx context.Context, email, name string) error {
	slog.InfoContext(ctx, "Sent welcome email", "email", email, "name", name)
	return nil
}

func (sm *StubMailer) SendOrgInvite(ctx context.Context, email, name string, orgName, orgOwnerEmail, orgOwnerName, orgURL string) error {
	slog.InfoContext(ctx, "Sent org invite email", "email", email, "name", name)
	return nil
}

func (sm *StubMailer) SendOrgRegisterInvite(ctx context.Context, email string, orgName, orgOwnerEmail, orgOwnerName, registerURL string) error {
	slog.InfoContext(ctx, "Sent org register invite email", "email", email, "orgName", orgName, "registerURL", registerURL)
	return nil
}
