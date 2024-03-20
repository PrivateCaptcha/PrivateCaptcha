package session

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Manager struct {
	CookieName  string
	Provider    Provider
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

func (m *Manager) SessionStart(w http.ResponseWriter, r *http.Request) (session Session) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		sid := m.sessionID()
		session, _ = m.Provider.SessionInit(sid)
		cookie := http.Cookie{
			Name:     m.CookieName,
			Value:    url.QueryEscape(sid),
			Path:     m.Path,
			HttpOnly: true,
			MaxAge:   int(m.MaxLifetime.Seconds()),
		}
		http.SetCookie(w, &cookie)
	} else {
		sid, _ := url.QueryUnescape(cookie.Value)
		session, _ = m.Provider.SessionRead(sid)
	}
	return
}

func (m *Manager) SessionDestroy(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(m.CookieName)
	if err != nil || cookie.Value == "" {
		return
	} else {
		m.Provider.SessionDestroy(cookie.Value)
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
	m.Provider.SessionGC(m.MaxLifetime)
	time.AfterFunc(m.MaxLifetime, func() { m.GC() })
}
