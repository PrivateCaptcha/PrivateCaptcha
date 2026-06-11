package session

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/rs/xid"
)

type Manager struct {
	CookieName   string
	Store        Store
	MaxLifetime  time.Duration
	Path         string
	SecureCookie bool
}

func (m *Manager) sessionID() string {
	return xid.New().String()
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

func (m *Manager) newSession(w http.ResponseWriter, r *http.Request) *Session {
	ctx := r.Context()
	sid := m.sessionID()
	sslog := slog.With(common.SessionIDAttr(sid))
	session := NewSession(NewSessionData(sid), m.Store)
	sslog.DebugContext(ctx, "Registering new session", "path", r.URL.Path, "method", r.Method)
	if err := m.Store.Init(ctx, session); err != nil {
		sslog.ErrorContext(ctx, "Failed to register session", common.ErrAttr(err))
	}
	m.setSessionCookie(w, r, sid, int(m.MaxLifetime.Seconds()))
	return session
}

func (m *Manager) Init(svc string, path string, interval time.Duration) {
	m.Path = path
	m.Store.Start(context.WithValue(context.Background(), common.ServiceContextKey, svc), interval)
}

func (m *Manager) SessionGet(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}

	sid, _ := url.QueryUnescape(cookie.Value)
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

func (m *Manager) SessionStart(w http.ResponseWriter, r *http.Request) (session *Session) {
	cookie, err := r.Cookie(m.CookieName)
	ctx := r.Context()
	if err != nil || cookie.Value == "" {
		slog.Log(ctx, common.LevelTrace, "Session cookie not found in the request for start", "path", r.URL.Path, "method", r.Method)
		session = m.newSession(w, r)
	} else {
		sid, _ := url.QueryUnescape(cookie.Value)
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

func (m *Manager) SessionRenew(w http.ResponseWriter, r *http.Request, sess *Session) *Session {
	ctx := r.Context()
	sid := m.sessionID()
	sslog := slog.With(common.SessionIDAttr(sid))
	renewed := NewSession(NewSessionData(sid), m.Store)
	renewed.Merge(sess)

	sslog.DebugContext(ctx, "Renewing session", "oldSessionID", sess.ID(), "path", r.URL.Path, "method", r.Method)
	if err := m.Store.Renew(ctx, sess.ID(), renewed); err != nil {
		sslog.ErrorContext(ctx, "Failed to register renewed session, continuing with current session", common.ErrAttr(err))
		return sess
	}
	m.setSessionCookie(w, r, sid, int(m.MaxLifetime.Seconds()))
	return renewed
}

func (m *Manager) RecoverSession(ctx context.Context, sess *Session) {
	if dbSess, err := m.Store.Read(ctx, sess.ID(), true /*skip cache*/); err == nil {
		sess.Refresh(dbSess)
	}
}

func (m *Manager) SessionDestroy(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		slog.Log(r.Context(), common.LevelTrace, "Session cookie not found in the request for destroy", "path", r.URL.Path, "method", r.Method)
		return
	} else {
		ctx := r.Context()
		slog.Log(ctx, common.LevelTrace, "Session cookie found in the request for destroy", common.SessionIDAttr(cookie.Value), "path", r.URL.Path, "method", r.Method)

		// NOTE: we can possibly do it in the background, but it's a rare action (only on logout) so it's not worth the complexity
		if err := m.Store.Destroy(ctx, cookie.Value); err != nil {
			slog.ErrorContext(ctx, "Failed to delete session from storage", common.ErrAttr(err))
		}

		expiration := time.Now()
		cookie := http.Cookie{
			Name:     m.CookieName,
			Path:     m.Path,
			HttpOnly: true,
			Expires:  expiration,
			Secure:   m.SecureCookie || (r.TLS != nil) || (r.Header.Get("X-Forwarded-Proto") == "https"),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		}
		http.SetCookie(w, &cookie)
		w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
	}
}
