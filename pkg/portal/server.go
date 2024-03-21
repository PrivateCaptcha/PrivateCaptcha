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
		TokenName         string
	}{
		LoginEndpoint:     common.LoginEndpoint,
		TwoFactorEndpoint: common.TwoFactorEndpoint,
		ResendEndpoint:    common.ResendEndpoint,
		TokenName:         common.ParamCsrfToken,
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
	router.HandleFunc(http.MethodGet+" "+prefix+common.TwoFactorEndpoint, s.getTwoFactor)
	router.HandleFunc(http.MethodPost+" "+prefix+common.TwoFactorEndpoint, common.Logged(s.postTwoFactor))
	router.HandleFunc(http.MethodPost+" "+prefix+common.ResendEndpoint, common.Logged(s.resend2fa))
	router.HandleFunc(http.MethodGet+" "+prefix+common.ErrorEndpoint+"/{code}", common.Logged(func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.PathValue("code"))
		s.renderError(r.Context(), w, code)
	}))
	router.HandleFunc(http.MethodGet+" "+prefix+"{$}", s.root)
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
	if err := s.template.Render(ctx, w, name, actualData); err != nil {
		slog.ErrorContext(ctx, "Failed to render template", common.ErrAttr(err))
		s.renderError(ctx, w, http.StatusInternalServerError)
	}
}

func (s *Server) htmxRedirectError(code int, w http.ResponseWriter, r *http.Request) {
	url := s.relURL(common.ErrorEndpoint + "/" + strconv.Itoa(code))
	s.htmxRedirect(url, w, r)
}

func (s *Server) renderError(ctx context.Context, w http.ResponseWriter, code int) {
	slog.DebugContext(ctx, "Rendering error page", "code", code)

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

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(r.Context(), w, http.StatusNotFound)
}

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); !ok || step != loginStepCompleted {
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	s.renderError(r.Context(), w, http.StatusNotImplemented)
}
