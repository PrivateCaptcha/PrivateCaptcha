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
		sid := m.sessionID()
		sslog := slog.With(common.SessionIDAttr(sid))
		session = NewSession(NewSessionData(sid), m.Store)
		sslog.DebugContext(ctx, "Registering new session", "path", r.URL.Path, "method", r.Method)
		if err = m.Store.Init(ctx, session); err != nil {
			sslog.ErrorContext(ctx, "Failed to register session", common.SessionIDAttr(sid), common.ErrAttr(err))
		}
		cookie := http.Cookie{
			Name:     m.CookieName,
			Value:    url.QueryEscape(sid),
			Path:     m.Path,
			HttpOnly: true,
			Secure:   m.SecureCookie || (r.TLS != nil) || (r.Header.Get("X-Forwarded-Proto") == "https"),
			MaxAge:   int(m.MaxLifetime.Seconds()),
		}
		http.SetCookie(w, &cookie)
		w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
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
			session = NewSession(NewSessionData(sid), m.Store)
			sslog.DebugContext(ctx, "Registering new session", "path", r.URL.Path, "method", r.Method)
			if err = m.Store.Init(ctx, session); err != nil {
				sslog.ErrorContext(ctx, "Failed to register session with existing cookie", common.ErrAttr(err))
			}
		}
	}
	return
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
			MaxAge:   -1,
		}
		http.SetCookie(w, &cookie)
		w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
	}
}
