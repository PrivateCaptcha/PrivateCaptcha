package portal

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

var (
	renderConstants = struct {
		LoginEndpoint     string
		TwoFactorEndpoint string
		ResendEndpoint    string
		RegisterEndpoint  string
		Token             string
		Email             string
		Name              string
		VerificationCode  string
	}{
		LoginEndpoint:     common.LoginEndpoint,
		TwoFactorEndpoint: common.TwoFactorEndpoint,
		ResendEndpoint:    common.ResendEndpoint,
		RegisterEndpoint:  common.RegisterEndpoint,
		Token:             common.ParamCsrfToken,
		Email:             common.ParamEmail,
		Name:              common.ParamName,
		VerificationCode:  common.ParamVerificationCode,
	}
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

	router.HandleFunc(http.MethodGet+" "+prefix+common.LoginEndpoint, s.getLogin)
	router.HandleFunc(http.MethodPost+" "+prefix+common.LoginEndpoint, common.Logged(s.postLogin))
	router.HandleFunc(http.MethodGet+" "+prefix+common.RegisterEndpoint, s.getRegister)
	router.HandleFunc(http.MethodPost+" "+prefix+common.RegisterEndpoint, common.Logged(s.postRegister))
	router.HandleFunc(http.MethodGet+" "+prefix+common.TwoFactorEndpoint, s.getTwoFactor)
	router.HandleFunc(http.MethodPost+" "+prefix+common.TwoFactorEndpoint, common.Logged(s.postTwoFactor))
	router.HandleFunc(http.MethodPost+" "+prefix+common.ResendEndpoint, common.Logged(s.resend2fa))
	router.HandleFunc(http.MethodGet+" "+prefix+common.ErrorEndpoint+"/{code}", s.error)
	router.HandleFunc(http.MethodGet+" "+prefix+common.ExpiredEndpoint, s.expired)
	router.HandleFunc(http.MethodGet+" "+prefix+"{$}", s.private(s.portal))
	router.HandleFunc(http.MethodGet+" "+prefix+"{path...}", common.Logged(s.notFound))
}

func (s *Server) render(ctx context.Context, w http.ResponseWriter, name string, data interface{}) {
	actualData := struct {
		Params interface{}
		Const  interface{}
	}{
		Params: data,
		Const:  renderConstants,
	}

	w.Header().Set(common.HeaderContentType, "text/html; charset=utf-8")

	if err := s.template.Render(ctx, w, name, actualData); err != nil {
		slog.ErrorContext(ctx, "Failed to render template", common.ErrAttr(err))
		s.renderError(ctx, w, http.StatusInternalServerError)
	}
}

func (s *Server) error(w http.ResponseWriter, r *http.Request) {
	code, _ := strconv.Atoi(r.PathValue("code"))
	s.renderError(r.Context(), w, code)
}

func (s *Server) htmxRedirectError(code int, w http.ResponseWriter, r *http.Request) {
	url := s.relURL(common.ErrorEndpoint + "/" + strconv.Itoa(code))
	s.htmxRedirect(url, w, r)
}

func (s *Server) htmxRedirect(url string, w http.ResponseWriter, r *http.Request) {
	slog.Debug("Redirecting using htmx header", "url", url)
	w.Header().Set("HX-Redirect", url)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(r.Context(), w, http.StatusNotFound)
}

func (s *Server) private(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.Session.SessionStart(w, r)

		if step, ok := sess.Get(session.KeyLoginStep).(int); ok {
			if step == loginStepCompleted {
				next.ServeHTTP(w, r)
				return
			} else {
				slog.WarnContext(r.Context(), "Session present, but login not finished", "step", step, "sid", sess.SessionID())
			}
		}

		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
	}
}
