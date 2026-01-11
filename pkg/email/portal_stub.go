package email

import (
	"context"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type StubMailer struct {
	LastCode  int
	LastEmail string
	Mailer    common.Mailer
}

var _ common.Mailer = (*StubMailer)(nil)

func (sm *StubMailer) SendTwoFactor(ctx context.Context, email string, code int, ua string, location string) error {
	sm.LastCode = code
	sm.LastEmail = email

	if sm.Mailer != nil {
		return sm.Mailer.SendTwoFactor(ctx, email, code, ua, location)
	}

	slog.InfoContext(ctx, "Sent two factor code via email", "code", code, "email", email)
	return nil
}

func (sm *StubMailer) SendWelcome(ctx context.Context, email, name string) error {
	if sm.Mailer != nil {
		return sm.Mailer.SendWelcome(ctx, email, name)
	}

	slog.InfoContext(ctx, "Sent welcome email", "email", email, "name", name)
	return nil
}

func (sm *StubMailer) SendOrgInvite(ctx context.Context, email, name string, orgName, orgOwnerEmail, orgOwnerName, orgURL string, requiresRegister bool) error {
	if sm.Mailer != nil {
		return sm.Mailer.SendOrgInvite(ctx, email, name, orgName, orgOwnerEmail, orgOwnerName, orgURL, requiresRegister)
	}

	slog.InfoContext(ctx, "Sent org invite email", "email", email, "name", name, "requiresRegister", requiresRegister)
	return nil
}
