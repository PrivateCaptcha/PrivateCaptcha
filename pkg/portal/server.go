package portal

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

var (
	renderConstants = struct {
		LoginEndpoint      string
		TwoFactorEndpoint  string
		ResendEndpoint     string
		RegisterEndpoint   string
		SettingsEndpoint   string
		LogoutEndpoint     string
		NewEndpoint        string
		OrgEndpoint        string
		PropertiesEndpoint string
		PropertyEndpoint   string
		Token              string
		Email              string
		Name               string
		VerificationCode   string
		Domain             string
		Difficulty         string
		Growth             string
	}{
		LoginEndpoint:      common.LoginEndpoint,
		TwoFactorEndpoint:  common.TwoFactorEndpoint,
		ResendEndpoint:     common.ResendEndpoint,
		RegisterEndpoint:   common.RegisterEndpoint,
		SettingsEndpoint:   common.SettingsEndpoint,
		LogoutEndpoint:     common.LogoutEndpoint,
		OrgEndpoint:        common.OrgEndpoint,
		PropertiesEndpoint: common.PropertiesEndpoint,
		PropertyEndpoint:   common.PropertyEndpoint,
		NewEndpoint:        common.NewEndpoint,
		Token:              common.ParamCsrfToken,
		Email:              common.ParamEmail,
		Name:               common.ParamName,
		VerificationCode:   common.ParamVerificationCode,
		Domain:             common.ParamDomain,
		Difficulty:         common.ParamDifficulty,
		Growth:             common.ParamGrowth,
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
		"partsURL": func(a ...string) any {
			return common.RelURL(prefix, strings.Join(a, "/"))
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

func (s *Server) Init() {
	prefix := s.relURL("/")
	s.template = web.NewTemplates(funcMap(prefix))
	s.Session.Path = prefix
}

func (s *Server) Setup(router *http.ServeMux) {
	s.setupWithPrefix(s.relURL("/"), router)
}

func (s *Server) relURL(url string) string {
	return common.RelURL(s.Prefix, url)
}

func (s *Server) partsURL(a ...string) string {
	return s.relURL(strings.Join(a, "/"))
}

func (s *Server) setupWithPrefix(prefix string, router *http.ServeMux) {
	slog.Debug("Setting up the routes", "prefix", prefix)

	router.HandleFunc(http.MethodGet+" "+prefix+common.LoginEndpoint, s.getLogin)
	router.HandleFunc(http.MethodPost+" "+prefix+common.LoginEndpoint, common.Logged(s.postLogin))
	router.HandleFunc(http.MethodGet+" "+prefix+common.RegisterEndpoint, s.getRegister)
	router.HandleFunc(http.MethodPost+" "+prefix+common.RegisterEndpoint, common.Logged(s.postRegister))
	router.HandleFunc(http.MethodGet+" "+prefix+common.TwoFactorEndpoint, s.getTwoFactor)
	router.HandleFunc(http.MethodPost+" "+prefix+common.TwoFactorEndpoint, common.Logged(s.postTwoFactor))
	router.HandleFunc(http.MethodPost+" "+prefix+common.ResendEndpoint, common.Logged(s.resend2fa))
	router.HandleFunc(http.MethodGet+" "+prefix+common.ErrorEndpoint+"/{code}", s.error)
	router.HandleFunc(http.MethodGet+" "+prefix+common.ExpiredEndpoint, s.expired)
	router.HandleFunc(http.MethodGet+" "+prefix+common.LogoutEndpoint, s.logout)
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}", s.private(s.org(s.getOrgDashboard)))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertiesEndpoint, s.private(s.org(s.getOrgProperties)))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/"+common.NewEndpoint, s.private(s.org(s.getNewOrgProperty)))
	router.HandleFunc(http.MethodPost+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/"+common.NewEndpoint, s.private(s.org(s.postNewOrgProperty)))
	router.HandleFunc(http.MethodGet+" "+prefix+"{$}", s.private(s.getOrgDashboard))
	router.HandleFunc(http.MethodGet+" "+prefix+"{path...}", common.Logged(s.notFound))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Session.SessionDestroy(w, r)
	common.Redirect(s.relURL(common.LoginEndpoint), w, r)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	ctx := r.Context()
	loggedIn, ok := ctx.Value(common.LoggedInContextKey).(bool)

	reqCtx := struct {
		Path     string
		LoggedIn bool
	}{
		Path:     r.URL.Path,
		LoggedIn: ok && loggedIn,
	}

	actualData := struct {
		Params interface{}
		Const  interface{}
		Ctx    interface{}
	}{
		Params: data,
		Const:  renderConstants,
		Ctx:    reqCtx,
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

func (s *Server) redirectError(code int, w http.ResponseWriter, r *http.Request) {
	url := s.relURL(common.ErrorEndpoint + "/" + strconv.Itoa(code))
	common.Redirect(url, w, r)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(r.Context(), w, http.StatusNotFound)
}

func (s *Server) org(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value := r.PathValue("org")

		orgID, err := strconv.Atoi(value)
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to parse org ID from path parameter", "value", value, common.ErrAttr(err))
			s.redirectError(http.StatusBadRequest, w, r)
			return
		}

		ctx := context.WithValue(r.Context(), common.OrgIDContextKey, orgID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) private(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.Session.SessionStart(w, r)

		if step, ok := sess.Get(session.KeyLoginStep).(int); ok {
			if step == loginStepCompleted {
				ctx := context.WithValue(r.Context(), common.LoggedInContextKey, true)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			} else {
				slog.WarnContext(r.Context(), "Session present, but login not finished", "step", step, "sid", sess.SessionID())
			}
		}

		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
	}
}
