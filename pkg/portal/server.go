package portal

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

const (
	maxLoginFormSizeBytes = 10 * 1024
	loginStepTwoFactor    = 1
	loginStepConfirmed    = 2
)

func funcMap(prefix string) template.FuncMap {
	return template.FuncMap{
		"qescape": url.QueryEscape,
		"safeHTML": func(s string) any {
			return template.HTML(s)
		},
		"relURL": func(s string) any {
			return common.RelURL(prefix, s)
		},
	}
}

type Server struct {
	Store    *db.Store
	Prefix   string
	template *web.Template
	XSRF     XSRFMiddleware
	Session  session.Manager
	Mailer   Mailer
}

func (s *Server) Setup(router *http.ServeMux) {
	s.setupWithPrefix(s.relURL("/"), router)
}

func (s *Server) relURL(url string) string {
	return common.RelURL(s.Prefix, url)
}

func (s *Server) setupWithPrefix(prefix string, router *http.ServeMux) {
	slog.Debug("Setting up the routes", "prefix", prefix)

	s.Session.Path = prefix
	s.template = web.NewTemplates(funcMap(prefix))

	router.HandleFunc(prefix+common.LoginEndpoint, common.Logged(s.login))
	router.HandleFunc(prefix+common.TwoFactorEndpoint, common.Logged(s.twofactor))
	router.HandleFunc(prefix+common.ResendEndpoint, common.Logged(common.Method(http.MethodPost, s.resend2fa)))
	router.HandleFunc(prefix, common.Logged(common.Method(http.MethodGet, s.root)))
}

func (s *Server) render(ctx context.Context, w http.ResponseWriter, name string, data interface{}) {
	if err := s.template.Render(ctx, w, name, data); err != nil {
		slog.ErrorContext(ctx, "Failed to render template", common.ErrAttr(err))
		s.renderError(ctx, w, http.StatusInternalServerError)
	}
}

func (s *Server) renderError(ctx context.Context, w http.ResponseWriter, code int) {
	data := struct {
		ErrorCode    int
		ErrorMessage string
		Detail       string
	}{
		ErrorCode:    code,
		ErrorMessage: http.StatusText(code),
	}

	switch code {
	case http.StatusNotFound:
		data.Detail = "This page does not exist."
	case http.StatusUnauthorized:
		data.Detail = "You need to log in to view this page."
	default:
		data.Detail = "Sorry, an unexpected error has occurred. Our team has been notified."
	}

	s.render(ctx, w, "errors/error.html", data)
}

func (s *Server) htmxRedirect(url string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Redirect", url)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != s.relURL("/") {
		s.renderError(r.Context(), w, http.StatusNotFound)
		return
	}

	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); !ok || step != loginStepConfirmed {
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	s.renderError(r.Context(), w, http.StatusNotImplemented)
}
