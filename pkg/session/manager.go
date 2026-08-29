package session

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

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
		HttpOnly: true,
		Secure:   m.SecureCookie || (r.TLS != nil) || (r.Header.Get("X-Forwarded-Proto") == "https"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
	http.SetCookie(w, &cookie)
	w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
}

func (m *Manager) prepareSession(r *http.Request) (*Session, error) {
	ctx := r.Context()
	sid := m.sessionID()
	sslog := slog.With(common.SessionIDAttr(sid))
	session := NewSession(NewSessionData(sid), m.Store)
	sslog.DebugContext(ctx, "Registering new session", "path", r.URL.Path, "method", r.Method)
	err := m.Store.Init(ctx, session)
	if err != nil {
		sslog.ErrorContext(ctx, "Failed to register session", common.ErrAttr(err))
	}
	return session, err
}

func (m *Manager) newSession(w http.ResponseWriter, r *http.Request) *Session {
	session, _ := m.prepareSession(r)
	m.setSessionCookie(w, r, session.ID(), int(m.MaxLifetime.Seconds()))
	return session
}

// SessionPrepare creates a process-local session without setting its cookie.
func (m *Manager) SessionPrepare(r *http.Request) (*Session, error) {
	return m.prepareSession(r)
}

// SessionCommit sets the prepared session cookie.
func (m *Manager) SessionCommit(w http.ResponseWriter, r *http.Request, sess *Session) {
	m.setSessionCookie(w, r, sess.ID(), int(m.MaxLifetime.Seconds()))
}

// Init starts the session store's background workers.
func (m *Manager) Init(svc string, path string, interval time.Duration) {
	m.Path = path
	m.Store.Start(context.WithValue(context.Background(), common.ServiceContextKey, svc), interval)
}

// SessionID returns the cookie SID without reading the store.
func (m *Manager) SessionID(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return decodeSessionID(cookie.Value)
}

func decodeSessionID(value string) (string, bool) {
	sid, err := url.QueryUnescape(value)
	return sid, err == nil && sid != "" && len(sid) <= 128 && utf8.ValidString(sid) && strings.IndexByte(sid, 0) == -1
}

// SessionGet reads through local cache and converts a missing cookie or any read error to false.
func (m *Manager) SessionGet(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}

	sid, valid := decodeSessionID(cookie.Value)
	if !valid {
		return nil, false
	}
	sslog := slog.With(common.SessionIDAttr(sid))

	ctx := r.Context()
	sslog.Log(ctx, common.LevelTrace, "Session cookie found in the request for start", "path", r.URL.Path, "method", r.Method)
	session, err := m.Store.Read(ctx, sid, false /*skip cache*/)
	if err != nil {
		level := slog.LevelWarn
		if err != ErrSessionMissing {
			level = slog.LevelError
		}
		sslog.Log(ctx, level, "Failed to read session from store", common.ErrAttr(err))

		return nil, false
	}

	return session, true
}

// SessionGetChecked reads through local cache.
// Unlike SessionGet, it returns missing/invalid-cookie and store errors.
func (m *Manager) SessionGetChecked(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		return nil, ErrSessionMissing
	}
	sid, valid := decodeSessionID(cookie.Value)
	if !valid {
		return nil, ErrSessionMissing
	}
	return m.Store.Read(r.Context(), sid, false /*skip cache*/)
}

// SessionStart reads through local cache and replaces a missing cookie or any failed read.
func (m *Manager) SessionStart(w http.ResponseWriter, r *http.Request) (session *Session) {
	cookie, err := r.Cookie(m.CookieName)
	ctx := r.Context()
	if err != nil || cookie.Value == "" {
		slog.Log(ctx, common.LevelTrace, "Session cookie not found in the request for start", "path", r.URL.Path, "method", r.Method)
		session = m.newSession(w, r)
	} else {
		sid, valid := decodeSessionID(cookie.Value)
		if !valid {
			slog.WarnContext(ctx, "Malformed session cookie found in the request for start", "path", r.URL.Path, "method", r.Method)
			return m.newSession(w, r)
		}
		sslog := slog.With(common.SessionIDAttr(sid))
		sslog.Log(ctx, common.LevelTrace, "Session cookie found in the request for start", "path", r.URL.Path, "method", r.Method)
		session, err = m.Store.Read(ctx, sid, false /*skip cache*/)
		if err != nil {
			level := slog.LevelWarn
			if err != ErrSessionMissing {
				level = slog.LevelError
			}
			sslog.Log(ctx, level, "Failed to read session from store", common.ErrAttr(err))
			session = m.newSession(w, r)
		}
	}
	return
}

// SessionStartChecked reads through local cache.
// It replaces a missing cookie/session but returns other store errors without replacement.
func (m *Manager) SessionStartChecked(w http.ResponseWriter, r *http.Request) (*Session, error) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		return m.newSession(w, r), nil
	}

	sid, valid := decodeSessionID(cookie.Value)
	if !valid {
		slog.WarnContext(r.Context(), "Malformed session cookie found in the request for checked start", "path", r.URL.Path, "method", r.Method)
		return m.newSession(w, r), nil
	}
	sslog := slog.With(common.SessionIDAttr(sid))
	ctx := r.Context()
	sslog.Log(ctx, common.LevelTrace, "Session cookie found in the request for checked start", "path", r.URL.Path, "method", r.Method)
	sess, err := m.Store.Read(ctx, sid, false /*skip cache*/)
	if err == nil {
		return sess, nil
	}
	if err == ErrSessionMissing {
		sslog.WarnContext(ctx, "Session is missing from store")
		return m.newSession(w, r), nil
	}

	sslog.ErrorContext(ctx, "Failed to read session from store", common.ErrAttr(err))
	return nil, err
}

// SessionRenew checks local cache before rotating a persistent session in PostgreSQL.
func (m *Manager) SessionRenew(w http.ResponseWriter, r *http.Request, sess *Session) *Session {
	ctx := r.Context()
	sid := m.sessionID()
	sslog := slog.With(common.SessionIDAttr(sid))
	renewed := NewSession(NewSessionData(sid), m.Store)

	sslog.DebugContext(ctx, "Renewing session", "oldSessionID", sess.ID(), "path", r.URL.Path, "method", r.Method)
	if err := m.Store.Renew(ctx, sess, renewed, func() { renewed.Merge(sess) }); err != nil {
		sslog.ErrorContext(ctx, "Failed to register renewed session, continuing with current session", common.ErrAttr(err))
		if sess.Data().IsStale() {
			return m.newSession(w, r)
		}
		return sess
	}
	m.setSessionCookie(w, r, sid, int(m.MaxLifetime.Seconds()))
	return renewed
}

// SessionConsumeSignInChallengeBlocking prepares and consumes the successor under SID ownership.
func (m *Manager) SessionConsumeSignInChallengeBlocking(w http.ResponseWriter, r *http.Request, sess *Session, encodedCode string, maxFailedAttempts int32, completedStep int) (*Session, SignInChallengeResult, error) {
	sid := m.sessionID()
	successor := NewSession(NewSessionData(sid), m.Store)
	result, err := m.Store.ConsumeSignInChallenge(r.Context(), sess, successor, func() {
		prepareChallengeSuccessor(successor, sess, completedStep)
		successor.data.delete(KeyUserEmail)
		successor.data.delete(KeyVerifyRegistration)
	}, encodedCode, maxFailedAttempts)
	if err != nil || !result.Consumed {
		return nil, result, err
	}
	m.setSessionCookie(w, r, sid, int(m.MaxLifetime.Seconds()))
	return successor, result, nil
}

// SessionConsumeRegistrationChallengeBlocking prepares and consumes the successor under SID ownership.
func (m *Manager) SessionConsumeRegistrationChallengeBlocking(r *http.Request, sess *Session, encodedCode string, maxFailedAttempts int32, allowConsumption bool, completedStep int) (*Session, RegistrationChallengeResult, error) {
	sid := m.sessionID()
	successor := NewSession(NewSessionData(sid), m.Store)
	result, err := m.Store.ConsumeRegistrationChallenge(r.Context(), sess, successor, func() {
		prepareChallengeSuccessor(successor, sess, completedStep)
	}, encodedCode, maxFailedAttempts, allowConsumption)
	if err != nil || !result.Consumed {
		return nil, result, err
	}
	successor.data.set(KeyUserEmail, result.Email)
	return successor, result, nil
}

func prepareChallengeSuccessor(successor, current *Session, completedStep int) {
	successor.Merge(current)
	successor.data.set(KeyLoginStep, completedStep)
	successor.data.set(KeyPersistent, true)
	successor.data.delete(KeyLoginAttempts)
	successor.data.delete(KeyTwoFactorCode)
	successor.data.delete(KeyTwoFactorCodeTimestamp)
}

// SessionAuthenticateRegistration authenticates locally without waiting for PostgreSQL.
func (m *Manager) SessionAuthenticateRegistration(w http.ResponseWriter, r *http.Request, sess *Session, userID int32, completedStep int) error {
	state, _, _ := sess.data.Authority()
	if userID <= 0 || state != StateRegistrationProcessing {
		return ErrSessionMissing
	}
	sess.data.set(KeyUserID, userID)
	sess.data.set(KeyFirstSession, true)
	sess.data.set(KeyLoginStep, completedStep)
	sess.data.set(KeyPersistent, true)
	sess.data.delete(KeyLoginAttempts)
	sess.data.delete(KeyTwoFactorCode)
	sess.data.delete(KeyTwoFactorCodeTimestamp)
	sess.data.delete(KeyUserEmail)
	sess.data.delete(KeyVerifyRegistration)
	if !sess.data.MarkRegistrationAuthenticatedLocally(time.Now().UTC()) {
		return ErrSessionMissing
	}
	m.setSessionCookie(w, r, sess.ID(), int(m.MaxLifetime.Seconds()))
	return nil
}

// SessionRefreshExpiration queues a heartbeat without waiting for PostgreSQL.
func (m *Manager) SessionRefreshExpiration(w http.ResponseWriter, r *http.Request, sess *Session) bool {
	if !m.Store.RenewExpiration(r.Context(), sess) {
		return false
	}
	m.setSessionCookie(w, r, sess.ID(), int(m.MaxLifetime.Seconds()))
	return true
}

// RecoverSessionBlocking reloads PostgreSQL without a cache lookup and ignores read errors.
func (m *Manager) RecoverSessionBlocking(ctx context.Context, sess *Session) {
	_ = m.Store.Recover(ctx, sess)
}

func (m *Manager) expireSessionCookie(w http.ResponseWriter, r *http.Request) {
	cookie := http.Cookie{
		Name:     m.CookieName,
		Path:     m.Path,
		HttpOnly: true,
		Expires:  time.Now(),
		Secure:   m.SecureCookie || (r.TLS != nil) || (r.Header.Get("X-Forwarded-Proto") == "https"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
	http.SetCookie(w, &cookie)
	w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
}

// SessionDestroy checks local cache before any PostgreSQL revocation.
func (m *Manager) SessionDestroy(w http.ResponseWriter, r *http.Request) (SessionRevocationResult, error) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil {
		slog.Log(r.Context(), common.LevelTrace, "Session cookie not found in the request for destroy", "path", r.URL.Path, "method", r.Method)
		return SessionRevocationResult{}, nil
	}
	sid, valid := decodeSessionID(cookie.Value)
	if !valid {
		slog.WarnContext(r.Context(), "Invalid session cookie found in the request for destroy", "path", r.URL.Path, "method", r.Method)
		m.expireSessionCookie(w, r)
		return SessionRevocationResult{}, nil
	}

	ctx := r.Context()
	slog.Log(ctx, common.LevelTrace, "Session cookie found in the request for destroy", common.SessionIDAttr(sid), "path", r.URL.Path, "method", r.Method)
	result, err := m.Store.Destroy(ctx, sid)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to revoke session in storage", common.ErrAttr(err))
		return SessionRevocationResult{}, err
	}

	m.expireSessionCookie(w, r)
	return result, nil
}
