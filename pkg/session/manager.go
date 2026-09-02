package session

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const sessionExpirationRenewalWindow = 2*time.Hour + 30*time.Minute

type Manager struct {
	CookieName   string
	Store        Store
	MaxLifetime  time.Duration
	Path         string
	SecureCookie bool
}

func (m *Manager) sessionID() string {
	return rand.Text()
}

func (m *Manager) setSessionCookie(w http.ResponseWriter, r *http.Request, sid string, maxAge int) {
	cookie := http.Cookie{
		Name:     m.CookieName,
		Value:    url.QueryEscape(sid),
		Path:     m.Path,
		Expires:  time.Now().Add(m.MaxLifetime),
		HttpOnly: true,
		Secure:   m.SecureCookie || (r.TLS != nil) || (r.Header.Get("X-Forwarded-Proto") == "https"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
	http.SetCookie(w, &cookie)
	w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
}

func (m *Manager) requestSessionID(r *http.Request) (string, error) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		return "", ErrSessionMissing
	}
	sid, err := url.QueryUnescape(cookie.Value)
	if err != nil || sid == "" {
		return "", ErrSessionMissing
	}
	return sid, nil
}

// Get resolves the presented SID through the dedicated session cache.
func (m *Manager) Get(r *http.Request) (*Session, error) {
	sid, err := m.requestSessionID(r)
	if err != nil {
		return nil, err
	}
	return m.Store.Resolve(r.Context(), sid)
}

// Start returns the presented session or creates a process-local anonymous one.
func (m *Manager) Start(w http.ResponseWriter, r *http.Request) (*Session, error) {
	sess, err := m.Get(r)
	if err == nil {
		return sess, nil
	}
	if !errors.Is(err, ErrSessionMissing) {
		return nil, err
	}

	sid := m.sessionID()
	sess = m.Store.StartAnonymousSession(sid)
	m.setSessionCookie(w, r, sid, int(m.MaxLifetime.Seconds()))
	return sess, nil
}

func (m *Manager) IssueSignInChallenge(w http.ResponseWriter, r *http.Request, sess *Session, userID int32, code string, challengeTTL time.Duration, maxAttempts int32) (*ChallengeResult, error) {
	payload, err := sess.Payload().Snapshot()
	if err != nil {
		return nil, err
	}
	result, err := m.Store.IssueSignInChallenge(sessionContext(r.Context(), sess), SignInChallengeIssue{
		SessionID:     sess.ID(),
		UserID:        userID,
		ChallengeCode: code,
		Payload:       payload,
		SessionTTL:    m.MaxLifetime,
		ChallengeTTL:  challengeTTL,
		MaxAttempts:   maxAttempts,
	})
	if err == nil && result != nil && result.Outcome == TransitionSucceeded && result.Session != nil {
		m.setSessionCookie(w, r, result.Session.ID(), int(m.MaxLifetime.Seconds()))
	}
	return result, err
}

func (m *Manager) IssueRegistrationChallenge(w http.ResponseWriter, r *http.Request, sess *Session, email, code string, requiresVerification bool, challengeTTL time.Duration, maxAttempts int32) (*ChallengeResult, error) {
	payload, err := sess.Payload().Snapshot()
	if err != nil {
		return nil, err
	}
	inviteID, _ := sess.Payload().Get(KeyOrgInviteID).(int32)
	result, err := m.Store.IssueRegistrationChallenge(sessionContext(r.Context(), sess), RegistrationChallengeIssue{
		SessionID:            sess.ID(),
		ChallengeEmail:       email,
		ChallengeCode:        code,
		Payload:              payload,
		RequiresVerification: requiresVerification,
		InviteID:             inviteID,
		SessionTTL:           m.MaxLifetime,
		ChallengeTTL:         challengeTTL,
		MaxAttempts:          maxAttempts,
	})
	if err == nil && result != nil && result.Outcome == TransitionSucceeded && result.Session != nil {
		m.setSessionCookie(w, r, result.Session.ID(), int(m.MaxLifetime.Seconds()))
	}
	return result, err
}

func (m *Manager) ResendPendingChallenge(ctx context.Context, sess *Session, code string, challengeTTL time.Duration, maxAttempts int32) (*ChallengeResult, error) {
	return m.Store.ResendPendingChallenge(sessionContext(ctx, sess), PendingChallengeResend{
		SessionID:     sess.ID(),
		ChallengeCode: code,
		ChallengeTTL:  challengeTTL,
		MaxAttempts:   maxAttempts,
	})
}

func (m *Manager) ConsumeSignInChallenge(w http.ResponseWriter, r *http.Request, sess *Session, code string, maxAttempts int32) (*ChallengeResult, error) {
	payload, err := sess.Payload().Snapshot()
	if err != nil {
		return nil, err
	}
	result, err := m.Store.ConsumeSignInChallenge(sessionContext(r.Context(), sess), SignInChallengeConsume{
		SessionID:          sess.ID(),
		SuccessorSessionID: m.sessionID(),
		ChallengeCode:      code,
		SuccessorPayload:   payload,
		SuccessorTTL:       m.MaxLifetime,
		MaxAttempts:        maxAttempts,
	})
	if err == nil && result != nil && result.Outcome == TransitionSucceeded && result.Session != nil {
		m.setSessionCookie(w, r, result.Session.ID(), int(m.MaxLifetime.Seconds()))
	}
	return result, err
}

func (m *Manager) ConsumeRegistrationChallenge(ctx context.Context, sess *Session, code string, maxAttempts int32) (*RegistrationConsumeResult, error) {
	return m.Store.ConsumeRegistrationChallenge(sessionContext(ctx, sess), RegistrationChallengeConsume{
		SessionID:     sess.ID(),
		ChallengeCode: code,
		MaxAttempts:   maxAttempts,
	})
}

func (m *Manager) CreateRegistrationSuccessor(w http.ResponseWriter, r *http.Request, predecessor *Session, userID int32) (*ChallengeResult, error) {
	payload, err := predecessor.Payload().Snapshot()
	if err != nil {
		return nil, err
	}
	result, err := m.Store.CreateRegistrationSuccessor(sessionContext(r.Context(), predecessor), RegistrationSuccessorCreate{
		SessionID: m.sessionID(),
		UserID:    userID,
		Payload:   payload,
		TTL:       m.MaxLifetime,
	})
	if err == nil && result != nil && result.Outcome == TransitionSucceeded && result.Session != nil {
		m.setSessionCookie(w, r, result.Session.ID(), int(m.MaxLifetime.Seconds()))
	}
	return result, err
}

func (m *Manager) IssueEmailChangeChallenge(ctx context.Context, sess *Session, code string, challengeTTL time.Duration) (*ChallengeResult, error) {
	return m.Store.IssueEmailChangeChallenge(sessionContext(ctx, sess), EmailChangeChallengeIssue{
		SessionID:     sess.ID(),
		ChallengeCode: code,
		ChallengeTTL:  challengeTTL,
	})
}

func (m *Manager) ConsumeEmailChangeChallenge(ctx context.Context, sess *Session, code string, maxAttempts int32) (*ChallengeResult, error) {
	return m.Store.ConsumeEmailChangeChallenge(sessionContext(ctx, sess), EmailChangeChallengeConsume{
		SessionID:     sess.ID(),
		ChallengeCode: code,
		MaxAttempts:   maxAttempts,
	})
}

func (m *Manager) Revoke(w http.ResponseWriter, r *http.Request) (*RevocationResult, error) {
	sid, err := m.requestSessionID(r)
	if errors.Is(err, ErrSessionMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hash := common.HashSessionID(sid)
	ctx := context.WithValue(r.Context(), common.SessionHashContextKey, hash)
	result, err := m.Store.RevokeSession(ctx, sid)
	if err != nil {
		return nil, err
	}
	if result != nil {
		result.SessionHash = hash
	}
	m.ClearCookie(w, r)
	return result, nil
}

func (m *Manager) RevokeUserSessions(ctx context.Context, userID int32) error {
	return m.Store.RevokeUserSessions(ctx, userID)
}

func (m *Manager) ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.CookieName,
		Path:     m.Path,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   m.SecureCookie || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
}

func (m *Manager) Init(svc string, path string, interval time.Duration) {
	m.Path = path
	m.Store.Start(context.WithValue(context.Background(), common.ServiceContextKey, svc), interval)
}

// ScheduleExpirationRenewal refreshes the cookie and queues persistence without request-time I/O.
func (m *Manager) ScheduleExpirationRenewal(w http.ResponseWriter, r *http.Request, sess *Session) {
	authority, ok := sess.Authority()
	if !ok || !shouldScheduleExpirationRenewal(authority, time.Now()) {
		return
	}

	m.Store.EnqueueExpirationRenewal(r.Context(), sess.ID())
	m.setSessionCookie(w, r, sess.ID(), int(m.MaxLifetime.Seconds()))
}

func shouldScheduleExpirationRenewal(authority Authority, now time.Time) bool {
	return authority.State == StateAuthenticated && now.Before(authority.ExpiresAt) && !authority.ExpiresAt.After(now.Add(sessionExpirationRenewalWindow))
}

func sessionContext(ctx context.Context, sess *Session) context.Context {
	return context.WithValue(ctx, common.SessionHashContextKey, sess.Hash())
}
