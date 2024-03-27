package session

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type Manager struct {
	CookieName  string
	Store       common.SessionStore
	MaxLifetime time.Duration
	Path        string
}

func (m *Manager) sessionID() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(b)
}

func (m *Manager) SessionStart(w http.ResponseWriter, r *http.Request) (session *common.Session) {
	cookie, err := r.Cookie(m.CookieName)
	ctx := r.Context()
	if err != nil || cookie.Value == "" {
		slog.Log(ctx, common.LevelTrace, "Session cookie not found in the request", "path", r.URL.Path, "method", r.Method)
		sid := m.sessionID()
		session = common.NewSession(sid, m.Store)
		if err = m.Store.Init(ctx, session); err != nil {
			slog.ErrorContext(ctx, "Failed to register session", "sid", sid, common.ErrAttr(err))
		}
		cookie := http.Cookie{
			Name:     m.CookieName,
			Value:    url.QueryEscape(sid),
			Path:     m.Path,
			HttpOnly: true,
			MaxAge:   int(m.MaxLifetime.Seconds()),
		}
		http.SetCookie(w, &cookie)
		w.Header().Add("Cache-Control", `no-cache="Set-Cookie"`)
	} else {
		sid, _ := url.QueryUnescape(cookie.Value)
		slog.Log(ctx, common.LevelTrace, "Session cookie found in the request", "sid", sid, "path", r.URL.Path, "method", r.Method)
		session, err = m.Store.Read(ctx, sid)
		if err == common.ErrSessionMissing {
			slog.WarnContext(ctx, "Session from cookie is missing", "sid", sid)
			session = common.NewSession(sid, m.Store)
			if err = m.Store.Init(ctx, session); err != nil {
				slog.ErrorContext(ctx, "Failed to register session with existing cookie", "sid", sid, common.ErrAttr(err))
			}
		} else if err != nil {
			slog.ErrorContext(ctx, "Failed to read session from store", common.ErrAttr(err))
		}
	}
	return
}

func (m *Manager) SessionDestroy(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		return
	} else {
		ctx := r.Context()
		if err := m.Store.Destroy(ctx, cookie.Value); err != nil {
			slog.ErrorContext(ctx, "Failed to delete session from storage", common.ErrAttr(err))
		}
		expiration := time.Now()
		cookie := http.Cookie{
			Name:     m.CookieName,
			Path:     m.Path,
			HttpOnly: true,
			Expires:  expiration,
			MaxAge:   -1,
		}
		http.SetCookie(w, &cookie)
	}
}

func (m *Manager) GC() {
	m.Store.GC(m.MaxLifetime)
	time.AfterFunc(m.MaxLifetime, func() { m.GC() })
}
