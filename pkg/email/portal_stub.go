package email

import (
	"context"
	"log/slog"
	"sync"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type StubMailer struct {
	LastCode  int
	LastEmail string
	Mailer    common.Mailer
	mu        sync.RWMutex
	codes     map[string]int
	sentCodes uint
}

var _ common.Mailer = (*StubMailer)(nil)

func (sm *StubMailer) SendTwoFactor(ctx context.Context, email string, code int, ua string, location string, isRegistration bool) error {
	sm.mu.Lock()
	sm.LastCode = code
	sm.LastEmail = email
	if sm.codes == nil {
		sm.codes = make(map[string]int)
	}
	sm.codes[email] = code
	sm.sentCodes++
	sm.mu.Unlock()

	if sm.Mailer != nil {
		return sm.Mailer.SendTwoFactor(ctx, email, code, ua, location, isRegistration)
	}

	slog.InfoContext(ctx, "Sent two factor code via email", "email", email)
	return nil
}

func (sm *StubMailer) TwoFactorCode(email string) (int, bool) {
	sm.mu.RLock()
	code, ok := sm.codes[email]
	sm.mu.RUnlock()
	return code, ok
}

func (sm *StubMailer) TwoFactorCount() uint {
	sm.mu.RLock()
	count := sm.sentCodes
	sm.mu.RUnlock()
	return count
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
