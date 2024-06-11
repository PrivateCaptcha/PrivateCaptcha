package portal

import (
	"bytes"
	"context"
	"fmt"
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
		APIKeysEndpoint      string
		Months               string
		SupportEndpoint      string
		Message              string
		Category             string
		BillingEndpoint      string
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
		APIKeysEndpoint:      common.APIKeysEndpoint,
		Months:               common.ParamMonths,
		SupportEndpoint:      common.SupportEndpoint,
		Message:              common.ParamMessage,
		Category:             common.ParamCategory,
		BillingEndpoint:      common.BillingEndpoint,
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

type Model = any
type ModelFunc func(http.ResponseWriter, *http.Request) (Model, string, error)

type requestContext struct {
	Path        string
	LoggedIn    bool
	CurrentYear int
	UserName    string
}

type alertRenderContext struct {
	ErrorMessage   string
	SuccessMessage string
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

	key := func(next http.HandlerFunc) http.HandlerFunc {
		return common.IntArg(next, "key", common.KeyIDContextKey, badRequestURL)
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

	router.HandleFunc(get(common.LoginEndpoint), s.handler(s.getLogin))
	router.HandleFunc(post(common.LoginEndpoint), common.Logged(s.postLogin))
	router.HandleFunc(get(common.RegisterEndpoint), s.handler(s.getRegister))
	router.HandleFunc(post(common.RegisterEndpoint), common.Logged(s.postRegister))
	router.HandleFunc(get(common.TwoFactorEndpoint), s.getTwoFactor)
	router.HandleFunc(post(common.TwoFactorEndpoint), common.Logged(s.postTwoFactor))
	router.HandleFunc(post(common.ResendEndpoint), common.Logged(s.resend2fa))
	router.HandleFunc(get(common.ErrorEndpoint, "{code}"), s.error)
	router.HandleFunc(get(common.ExpiredEndpoint), s.expired)
	router.HandleFunc(get(common.LogoutEndpoint), s.logout)
	router.HandleFunc(get(common.OrgEndpoint, common.NewEndpoint), s.private(s.handler(s.getNewOrg)))
	router.HandleFunc(post(common.OrgEndpoint, common.NewEndpoint), common.Logged(s.private(s.postNewOrg)))
	router.HandleFunc(get(common.OrgEndpoint, "{org}"), s.private(org(s.getPortal)))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.TabEndpoint, common.DashboardEndpoint), s.private(org(s.handler(s.getOrgDashboard))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.TabEndpoint, common.MembersEndpoint), s.private(org(s.handler(s.getOrgMembers))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.TabEndpoint, common.SettingsEndpoint), s.private(org(s.handler(s.getOrgSettings))))
	router.HandleFunc(post(common.OrgEndpoint, "{org}", common.MembersEndpoint), s.private(org(s.postOrgMembers)))
	router.HandleFunc(delete(common.OrgEndpoint, "{org}", common.MembersEndpoint, "{user}"), s.private(org(user(s.deleteOrgMembers))))
	router.HandleFunc(put(common.OrgEndpoint, "{org}", common.MembersEndpoint), s.private(org(s.joinOrg)))
	router.HandleFunc(delete(common.OrgEndpoint, "{org}", common.MembersEndpoint), s.private(org(s.leaveOrg)))
	router.HandleFunc(put(common.OrgEndpoint, "{org}", common.EditEndpoint), s.private(org(s.putOrg)))
	router.HandleFunc(delete(common.OrgEndpoint, "{org}", common.DeleteEndpoint), s.private(org(s.deleteOrg)))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, common.NewEndpoint), s.private(org(s.subscribed(s.handler(s.getNewOrgProperty)))))
	router.HandleFunc(post(common.OrgEndpoint, "{org}", common.PropertyEndpoint, common.NewEndpoint), common.Logged(s.private(org(s.postNewOrgProperty))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}"), s.private(org(property(s.handler(s.getPropertyDashboard)))))
	router.HandleFunc(put(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.EditEndpoint), s.private(org(property(s.putProperty))))
	router.HandleFunc(delete(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.DeleteEndpoint), s.private(org(property(s.deleteProperty))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.TabEndpoint, common.ReportsEndpoint), s.private(org(property(s.handler(s.getPropertyReports)))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.TabEndpoint, common.SettingsEndpoint), s.private(org(property(s.handler(s.getPropertySettings)))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.TabEndpoint, common.IntegrationsEndpoint), s.private(org(property(s.handler(s.getPropertyIntegrations)))))
	router.HandleFunc(get(common.OrgEndpoint, "{org}", common.PropertyEndpoint, "{property}", common.StatsEndpoint, "{period}"), s.private(org(property(period(s.getPropertyStats)))))
	router.HandleFunc(get(common.SettingsEndpoint), s.private(s.handler(s.getSettings)))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), s.private(s.handler(s.getGeneralSettings)))
	router.HandleFunc(post(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint, common.EmailEndpoint), s.private(s.editEmail))
	router.HandleFunc(put(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), s.private(s.putGeneralSettings))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint), s.private(s.handler(s.getAPIKeysSettings)))
	router.HandleFunc(post(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint, common.NewEndpoint), s.private(s.postAPIKeySettings))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint), s.private(s.handler(s.getBillingSettings)))
	router.HandleFunc(delete(common.APIKeysEndpoint, "{key}"), s.private(key(s.deleteAPIKey)))
	router.HandleFunc(delete(common.UserEndpoint), s.private(s.deleteAccount))
	router.HandleFunc(get(common.SupportEndpoint), s.private(s.handler(s.getSupport)))
	router.HandleFunc(post(common.SupportEndpoint), s.private(s.postSupport))
	router.HandleFunc(http.MethodGet+" "+prefix+"{$}", s.private(s.getPortal))
	router.HandleFunc(http.MethodGet+" "+prefix+"{path...}", common.Logged(s.notFound))
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) *common.Session {
	ctx := r.Context()
	sess, ok := ctx.Value(common.SessionContextKey).(*common.Session)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get session from context")
		sess = s.Session.SessionStart(w, r)
	}
	return sess
}

func (s *Server) sessionUser(w http.ResponseWriter, r *http.Request) (*dbgen.User, error) {
	ctx := r.Context()
	sess := s.session(w, r)

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		return nil, errInvalidSession
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", "email", email, common.ErrAttr(err))
		return nil, err
	}

	return user, nil
}

func (s *Server) subscribed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		user, err := s.sessionUser(w, r)
		if err != nil {
			common.Redirect(s.relURL(common.LoginEndpoint), w, r)
			return
		}

		if !user.SubscriptionID.Valid {
			slog.WarnContext(ctx, "User does not have a subscription", "userID", user.ID)
			url := s.relURL(fmt.Sprintf("%s?%s=%s", common.SettingsEndpoint, common.ParamTab, common.BillingEndpoint))
			common.Redirect(url, w, r)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Session.SessionDestroy(w, r)
	common.Redirect(s.relURL(common.LoginEndpoint), w, r)
}

func (s *Server) handler(modelFunc ModelFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// such composition makes business logic and rendering testable separately
		renderCtx, tpl, err := modelFunc(w, r)
		if err != nil {
			if err == errInvalidSession {
				common.Redirect(s.relURL(common.LoginEndpoint), w, r)
			} else {
				slog.ErrorContext(ctx, "Failed to create model for request", common.ErrAttr(err))
				s.redirectError(http.StatusInternalServerError, w, r)
			}
			return
		}

		s.render(w, r, tpl, renderCtx)
	}
}

func (s *Server) renderResponse(ctx context.Context, name string, data interface{}, reqCtx *requestContext) (bytes.Buffer, error) {
	actualData := struct {
		Params interface{}
		Const  interface{}
		Ctx    interface{}
	}{
		Params: data,
		Const:  renderConstants,
		Ctx:    reqCtx,
	}

	var out bytes.Buffer
	err := s.template.Render(ctx, &out, name, actualData)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to render template", common.ErrAttr(err))
	}

	return out, err
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	ctx := r.Context()

	loggedIn, ok := ctx.Value(common.LoggedInContextKey).(bool)

	reqCtx := &requestContext{
		Path:        r.URL.Path,
		LoggedIn:    ok && loggedIn,
		CurrentYear: time.Now().Year(),
	}

	sess := s.Session.SessionStart(w, r)
	if username, ok := sess.Get(session.KeyUserName).(string); ok {
		reqCtx.UserName = username
	}

	out, err := s.renderResponse(ctx, name, data, reqCtx)
	if err == nil {
		w.Header().Set(common.HeaderContentType, "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		out.WriteTo(w)
	} else {
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
				ctx = context.WithValue(ctx, common.SessionContextKey, sess)

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			} else {
				slog.WarnContext(r.Context(), "Session present, but login not finished", "step", step, "sid", sess.SessionID())
			}
		}

		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
	}
}
