package portal

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

var (
	renderConstants = struct {
		LoginEndpoint        string
		TwoFactorEndpoint    string
		ResendEndpoint       string
		RegisterEndpoint     string
		SettingsEndpoint     string
		LogoutEndpoint       string
		NewEndpoint          string
		OrgEndpoint          string
		PropertyEndpoint     string
		DashboardEndpoint    string
		TabEndpoint          string
		ReportsEndpoint      string
		IntegrationsEndpoint string
		EditEndpoint         string
		Token                string
		Email                string
		Name                 string
		VerificationCode     string
		Domain               string
		Difficulty           string
		Growth               string
		Stats                string
	}{
		LoginEndpoint:        common.LoginEndpoint,
		TwoFactorEndpoint:    common.TwoFactorEndpoint,
		ResendEndpoint:       common.ResendEndpoint,
		RegisterEndpoint:     common.RegisterEndpoint,
		SettingsEndpoint:     common.SettingsEndpoint,
		LogoutEndpoint:       common.LogoutEndpoint,
		OrgEndpoint:          common.OrgEndpoint,
		PropertyEndpoint:     common.PropertyEndpoint,
		DashboardEndpoint:    common.DashboardEndpoint,
		NewEndpoint:          common.NewEndpoint,
		Token:                common.ParamCsrfToken,
		Email:                common.ParamEmail,
		Name:                 common.ParamName,
		VerificationCode:     common.ParamVerificationCode,
		Domain:               common.ParamDomain,
		Difficulty:           common.ParamDifficulty,
		Growth:               common.ParamGrowth,
		Stats:                common.StatsEndpoint,
		TabEndpoint:          common.TabEndpoint,
		ReportsEndpoint:      common.ReportsEndpoint,
		IntegrationsEndpoint: common.IntegrationsEndpoint,
		EditEndpoint:         common.EditEndpoint,
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
	Store    *db.BusinessStore
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

	badRequestURL := s.relURL(common.ErrorEndpoint + "/" + strconv.Itoa(http.StatusBadRequest))

	org := func(next http.HandlerFunc) http.HandlerFunc {
		return common.IntArg(next, "org", common.OrgIDContextKey, badRequestURL)
	}

	property := func(next http.HandlerFunc) http.HandlerFunc {
		return common.IntArg(next, "property", common.PropertyIDContextKey, badRequestURL)
	}

	period := func(next http.HandlerFunc) http.HandlerFunc {
		return common.StrArg(next, "period", common.PeriodContextKey, badRequestURL)
	}

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
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/"+common.NewEndpoint, s.private(s.getNewOrg))
	router.HandleFunc(http.MethodPost+" "+prefix+common.OrgEndpoint+"/"+common.NewEndpoint, common.Logged(s.private(s.postNewOrg)))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}", s.private(org(s.getPortal)))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.DashboardEndpoint, s.private(org(s.getOrgDashboard)))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/"+common.NewEndpoint, s.private(org(s.getNewOrgProperty)))
	router.HandleFunc(http.MethodPost+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/"+common.NewEndpoint, common.Logged(s.private(org(s.postNewOrgProperty))))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/{property}", s.private(org(property(s.getPropertyDashboard(propertyDashboardTemplate)))))
	router.HandleFunc(http.MethodPut+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/{property}/"+common.EditEndpoint, s.private(org(property(s.putProperty))))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/{property}/"+common.TabEndpoint+"/"+common.ReportsEndpoint, s.private(org(property(s.getPropertyDashboard(propertyDashboardReportsTemplate)))))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/{property}/"+common.TabEndpoint+"/"+common.SettingsEndpoint, s.private(org(property(s.getPropertyDashboard(propertyDashboardSettingsTemplate)))))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/{property}/"+common.TabEndpoint+"/"+common.IntegrationsEndpoint, s.private(org(property(s.getPropertyDashboard(propertyDashboardIntegrationsTemplate)))))
	router.HandleFunc(http.MethodGet+" "+prefix+common.OrgEndpoint+"/{org}/"+common.PropertyEndpoint+"/{property}/"+common.StatsEndpoint+"/{period}", s.private(org(property(period(s.getRandomPropertyStats)))))
	router.HandleFunc(http.MethodGet+" "+prefix+"{$}", s.private(s.getPortal))
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
		Path        string
		LoggedIn    bool
		CurrentYear int
		UserName    string
	}{
		Path:        r.URL.Path,
		LoggedIn:    ok && loggedIn,
		CurrentYear: time.Now().Year(),
	}

	sess := s.Session.SessionStart(w, r)
	if username, ok := sess.Get(session.KeyUserName).(string); ok {
		reqCtx.UserName = username
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
