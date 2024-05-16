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
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
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
		DeleteEndpoint       string
		MembersEndpoint      string
		OrgLevelInvited      string
		OrgLevelMember       string
		OrgLevelOwner        string
		GeneralEndpoint      string
		EmailEndpoint        string
		UserEndpoint         string
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
		DeleteEndpoint:       common.DeleteEndpoint,
		MembersEndpoint:      common.MembersEndpoint,
		OrgLevelInvited:      string(dbgen.AccessLevelInvited),
		OrgLevelMember:       string(dbgen.AccessLevelMember),
		OrgLevelOwner:        string(dbgen.AccessLevelOwner),
		GeneralEndpoint:      common.GeneralEndpoint,
		EmailEndpoint:        common.EmailEndpoint,
		UserEndpoint:         common.UserEndpoint,
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
	Store      *db.BusinessStore
	TimeSeries *db.TimeSeriesStore
	Prefix     string
	template   *web.Template
	XSRF       XSRFMiddleware
	Session    session.Manager
	Mailer     Mailer
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

	user := func(next http.HandlerFunc) http.HandlerFunc {
		return common.IntArg(next, "user", common.UserIDContextKey, badRequestURL)
	}

	period := func(next http.HandlerFunc) http.HandlerFunc {
		return common.StrArg(next, "period", common.PeriodContextKey, badRequestURL)
	}

	route := func(method string, parts ...string) string {
		return method + " " + prefix + strings.Join(parts, "/")
	}

	get := func(parts ...string) string {
		return route(http.MethodGet, parts...)
	}

	post := func(parts ...string) string {
		return route(http.MethodPost, parts...)
	}

	put := func(parts ...string) string {
		return route(http.MethodPut, parts...)
	}

	delete := func(parts ...string) string {
		return route(http.MethodDelete, parts...)
	}

	router.HandleFunc(get(common.LoginEndpoint), s.getLogin)
	router.HandleFunc(post(common.LoginEndpoint), common.Logged(s.postLogin))
	router.HandleFunc(get(common.RegisterEndpoint), s.getRegister)
	router.HandleFunc(post(common.RegisterEndpoint), common.Logged(s.postRegister))
	router.HandleFunc(get(common.TwoFactorEndpoint), s.getTwoFactor)
	router.HandleFunc(post(common.TwoFactorEndpoint), common.Logged(s.postTwoFactor))
	router.HandleFunc(post(common.ResendEndpoint), common.Logged(s.resend2fa))
	router.HandleFunc(get(common.ErrorEndpoint, "{code}"), s.error)
	router.HandleFunc(get(common.ExpiredEndpoint), s.expired)
	router.HandleFunc(get(common.LogoutEndpoint), s.logout)
	router.HandleFunc(get(common.OrgEndpoint, common.NewEndpoint), s.private(s.getNewOrg))
	router.HandleFunc(post(common.OrgEndpoint, common.NewEndpoint), common.Logged(s.private(s.postNewOrg)))
	router.HandleFunc(get(common.OrgEndpoint, "{org}"), s.private(org(s.getPortal)))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.TabEndpoint, common.DashboardEndpoint), s.private(org(s.getOrgDashboard)))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.TabEndpoint, common.MembersEndpoint), s.private(org(s.getOrgMembers)))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.TabEndpoint, common.SettingsEndpoint), s.private(org(s.getOrgSettings)))
	router.HandleFunc(post(common.OrgEndpoint, "{org}", common.MembersEndpoint), s.private(org(s.postOrgMembers)))
	router.HandleFunc(delete(common.OrgEndpoint, "{org}", common.MembersEndpoint, "{user}"), s.private(org(user(s.deleteOrgMembers))))
	router.HandleFunc(put(common.OrgEndpoint, "{org}", common.MembersEndpoint), s.private(org(s.joinOrg)))
	router.HandleFunc(delete(common.OrgEndpoint, "{org}", common.MembersEndpoint), s.private(org(s.leaveOrg)))
	router.HandleFunc(put(common.OrgEndpoint, "{org}", common.EditEndpoint), s.private(org(s.putOrg)))
	router.HandleFunc(delete(common.OrgEndpoint, "{org}", common.DeleteEndpoint), s.private(org(s.deleteOrg)))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, common.NewEndpoint), s.private(org(s.getNewOrgProperty)))
	router.HandleFunc(post(common.OrgEndpoint, "{org}", common.PropertyEndpoint, common.NewEndpoint), common.Logged(s.private(org(s.postNewOrgProperty))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}"), s.private(org(property(s.getPropertyDashboard(propertyDashboardTemplate)))))
	router.HandleFunc(put(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.EditEndpoint), s.private(org(property(s.putProperty))))
	router.HandleFunc(delete(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.DeleteEndpoint), s.private(org(property(s.deleteProperty))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.TabEndpoint, common.ReportsEndpoint), s.private(org(property(s.getPropertyDashboard(propertyDashboardReportsTemplate)))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.TabEndpoint, common.SettingsEndpoint), s.private(org(property(s.getPropertyDashboard(propertyDashboardSettingsTemplate)))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.TabEndpoint, common.IntegrationsEndpoint), s.private(org(property(s.getPropertyDashboard(propertyDashboardIntegrationsTemplate)))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.StatsEndpoint, "{period}"), s.private(org(property(period(s.getPropertyStats)))))
	router.HandleFunc(get(common.SettingsEndpoint), s.private(s.getGeneralSettings(settingsTemplate)))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), s.private(s.getGeneralSettings(settingsGeneralTemplate)))
	router.HandleFunc(post(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint, common.EmailEndpoint), s.private(s.editEmail))
	router.HandleFunc(put(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), s.private(s.putGeneralSettings))
	router.HandleFunc(delete(common.UserEndpoint), s.private(s.deleteAccount))
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
