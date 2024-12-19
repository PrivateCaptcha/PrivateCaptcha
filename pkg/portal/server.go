package portal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/justinas/alice"
	"github.com/rs/cors"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

var (
	errInvalidPathArg    = errors.New("path argument is not valid")
	errInvalidRequestArg = errors.New("request argument is not valid")

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
		Tab                  string
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
		Product              string
		CancelEndpoint       string
		UpdateEndpoint       string
		PreviewEndpoint      string
		Yearly               string
		Price                string
		HeaderCSRFToken      string
		UsageEndpoint        string
		NotificationEndpoint string
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
		Token:                common.ParamCSRFToken,
		Email:                common.ParamEmail,
		Name:                 common.ParamName,
		Tab:                  common.ParamTab,
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
		Product:              common.ParamProduct,
		CancelEndpoint:       common.CancelEndpoint,
		UpdateEndpoint:       common.UpdateEndpoint,
		PreviewEndpoint:      common.PreviewEndpoint,
		Yearly:               common.ParamYearly,
		Price:                common.ParamPrice,
		HeaderCSRFToken:      common.HeaderCSRFToken,
		UsageEndpoint:        common.UsageEndpoint,
		NotificationEndpoint: common.NotificationEndpoint,
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
		"plus1": func(x int) int {
			return x + 1
		},
	}
}

type CsrfKeyFunc func(http.ResponseWriter, *http.Request) string

type Model = any
type ModelFunc func(http.ResponseWriter, *http.Request) (Model, string, error)

type requestContext struct {
	Path        string
	LoggedIn    bool
	CurrentYear int
	UserName    string
	UserEmail   string
	CDN         string
}

type csrfRenderContext struct {
	Token string
}

type systemNotificationContext struct {
	Notification   string
	NotificationID string
}

type alertRenderContext struct {
	ErrorMessage   string
	SuccessMessage string
	WarningMessage string
	InfoMessage    string
}

func (ac *alertRenderContext) ClearAlerts() {
	ac.ErrorMessage = ""
	ac.SuccessMessage = ""
	ac.WarningMessage = ""
	ac.InfoMessage = ""
}

type Server struct {
	Store           *db.BusinessStore
	TimeSeries      *db.TimeSeriesStore
	APIURL          string
	CDNURL          string
	Prefix          string
	template        *web.Template
	XSRF            XSRFMiddleware
	Session         session.Manager
	Mailer          common.Mailer
	Stage           string
	PaddleAPI       billing.PaddleAPI
	cors            *cors.Cors
	Verifier        puzzle.Verifier
	Metrics         monitoring.Metrics
	maintenanceMode atomic.Bool
	canRegister     atomic.Bool
}

func (s *Server) Init() {
	prefix := common.RelURL(s.Prefix, "/")
	s.template = web.NewTemplates(funcMap(prefix))
	s.Session.Path = prefix
}

func (s *Server) UpdateConfig(config *config.Config) {
	s.maintenanceMode.Store(config.MaintenanceMode())
	s.canRegister.Store(config.RegistrationAllowed())
}

func (s *Server) Setup(router *http.ServeMux, domain string, ratelimiter alice.Constructor) {
	corsDomain := domain
	if len(corsDomain) == 0 {
		slog.Error("CORS portal server domain is empty")
		corsDomain = "*"
	}

	corsOpts := cors.Options{
		AllowedOrigins:   []string{corsDomain},
		AllowCredentials: true,
		// non-captcha headers were taken from rs/cors defaults
		AllowedHeaders: []string{common.HeaderCSRFToken, "accept", "content-type", "x-requested-with"},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodHead, http.MethodPut, http.MethodDelete},
		Debug:          s.Stage != common.StageProd,
		MaxAge:         60, /*seconds*/
	}

	if corsOpts.Debug {
		corsOpts.Logger = &common.FmtLogger{Ctx: common.TraceContext(context.TODO(), "cors"), Level: common.LevelTrace}
	}

	s.cors = cors.New(corsOpts)

	s.setupWithPrefix(domain+s.relURL("/"), router, ratelimiter, s.cors.Handler)
}

func (s *Server) relURL(url string) string {
	return common.RelURL(s.Prefix, url)
}

func (s *Server) partsURL(a ...string) string {
	return s.relURL(strings.Join(a, "/"))
}

// routeGenerator's point is to passthrough the path correctly to the std.Handler() of slok/go-http-metrics
// the whole magic can break if for some reason Go will not evaluate result of Route() before calling Alice's Then()
// when calling router.Handle() in setupWithPrefix()
type routeGenerator struct {
	prefix string
	path   string
}

func (rg *routeGenerator) Route(method string, parts ...string) string {
	rg.path = rg.prefix + strings.Join(parts, "/")
	return method + " " + rg.path
}

func (rg *routeGenerator) Get(parts ...string) string {
	return rg.Route(http.MethodGet, parts...)
}

func (rg *routeGenerator) Post(parts ...string) string {
	return rg.Route(http.MethodPost, parts...)
}

func (rg *routeGenerator) Put(parts ...string) string {
	return rg.Route(http.MethodPut, parts...)
}

func (rg *routeGenerator) Delete(parts ...string) string {
	return rg.Route(http.MethodDelete, parts...)
}

func (rg *routeGenerator) LastPath() string {
	result := rg.path
	// side-effect: this will cause go http metrics handler to use handlerID based on request Path
	rg.path = ""
	return result
}

func (s *Server) setupWithPrefix(prefix string, router *http.ServeMux, ratelimiter, corsHandler alice.Constructor) {
	slog.Debug("Setting up the portal routes", "prefix", prefix)

	rg := &routeGenerator{prefix: prefix}

	arg := func(s string) string {
		return fmt.Sprintf("{%s}", s)
	}

	maxBytesHandler := func(next http.Handler) http.Handler {
		return http.MaxBytesHandler(next, 256*1024)
	}

	// NOTE: with regards to CORS, for portal server we want CORS to be before rate limiting

	// separately configured "public" ones
	publicChain := alice.New(common.Recovered, s.Metrics.HandlerFunc(rg.LastPath), corsHandler, ratelimiter, monitoring.Logged)
	publicCachedChain := publicChain.Append(common.CacheControl)
	publicMaintenanceChain := publicChain.Append(s.maintenance)
	router.Handle(rg.Get(common.LoginEndpoint), publicMaintenanceChain.Then(s.handler(s.getLogin)))
	router.Handle(rg.Get(common.RegisterEndpoint), publicMaintenanceChain.Then(s.handler(s.getRegister)))
	router.Handle(rg.Get(common.TwoFactorEndpoint), publicMaintenanceChain.ThenFunc(s.getTwoFactor))
	router.Handle(rg.Get(common.ErrorEndpoint, arg(common.ParamCode)), publicChain.Append(common.CacheControl).ThenFunc(s.error))
	router.Handle(rg.Get(common.ExpiredEndpoint), publicCachedChain.ThenFunc(s.expired))
	router.Handle(rg.Get(common.LogoutEndpoint), publicChain.ThenFunc(s.logout))

	// configured with middlewares
	openWrite := publicChain.Append(maxBytesHandler, s.maintenance)
	csrfEmailChain := openWrite.Append(s.csrf(s.csrfUserEmailKeyFunc))
	privateWriteChain := openWrite.Append(s.csrf(s.csrfUserIDKeyFunc), s.private)
	subscribedWrite := privateWriteChain.Append(s.subscribed)

	privateReadChain := publicChain.Append(s.private)
	privateFreshReadChain := privateReadChain.Append(common.NoCache)
	subscribedRead := privateReadChain.Append(s.subscribed)

	router.Handle(rg.Post(common.LoginEndpoint), openWrite.ThenFunc(s.postLogin))
	router.Handle(rg.Post(common.RegisterEndpoint), openWrite.ThenFunc(s.postRegister))
	router.Handle(rg.Post(common.TwoFactorEndpoint), csrfEmailChain.ThenFunc(s.postTwoFactor))
	router.Handle(rg.Post(common.ResendEndpoint), csrfEmailChain.ThenFunc(s.resend2fa))
	router.Handle(rg.Get(common.OrgEndpoint, common.NewEndpoint), privateReadChain.Then(s.handler(s.getNewOrg)))
	router.Handle(rg.Post(common.OrgEndpoint, common.NewEndpoint), subscribedWrite.ThenFunc(s.postNewOrg))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg)), privateReadChain.ThenFunc(s.getPortal))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.DashboardEndpoint), privateReadChain.Then(s.handler(s.getOrgDashboard)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.MembersEndpoint), privateReadChain.Then(s.handler(s.getOrgMembers)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.SettingsEndpoint), privateReadChain.Then(s.handler(s.getOrgSettings)))
	router.Handle(rg.Post(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWriteChain.Then(s.handler(s.postOrgMembers)))
	router.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint, arg(common.ParamUser)), privateWriteChain.ThenFunc(s.deleteOrgMembers))
	router.Handle(rg.Put(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWriteChain.ThenFunc(s.joinOrg))
	router.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWriteChain.ThenFunc(s.leaveOrg))
	router.Handle(rg.Put(common.OrgEndpoint, arg(common.ParamOrg), common.EditEndpoint), privateWriteChain.Then(s.handler(s.putOrg)))
	router.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.DeleteEndpoint), privateWriteChain.ThenFunc(s.deleteOrg))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, common.NewEndpoint), subscribedRead.Then(s.handler(s.getNewOrgProperty)))
	router.Handle(rg.Post(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, common.NewEndpoint), subscribedWrite.ThenFunc(s.postNewOrgProperty))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty)), privateReadChain.Then(s.handler(s.getPropertyDashboard)))
	router.Handle(rg.Put(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.EditEndpoint), privateWriteChain.Then(s.handler(s.putProperty)))
	router.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.DeleteEndpoint), privateWriteChain.ThenFunc(s.deleteProperty))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.ReportsEndpoint), privateReadChain.Then(s.handler(s.getPropertyReportsTab)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.SettingsEndpoint), privateReadChain.Then(s.handler(s.getPropertySettingsTab)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.IntegrationsEndpoint), privateReadChain.Then(s.handler(s.getPropertyIntegrationsTab)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.StatsEndpoint, arg(common.ParamPeriod)), privateFreshReadChain.ThenFunc(s.getPropertyStats))
	router.Handle(rg.Get(common.SettingsEndpoint), privateReadChain.Then(s.handler(s.getSettings)))
	router.Handle(rg.Get(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), privateReadChain.Then(s.handler(s.getGeneralSettings)))
	router.Handle(rg.Post(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint, common.EmailEndpoint), privateWriteChain.Then(s.handler(s.editEmail)))
	router.Handle(rg.Put(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), privateWriteChain.Then(s.handler(s.putGeneralSettings)))
	router.Handle(rg.Get(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint), privateReadChain.Then(s.handler(s.getAPIKeysSettings)))
	router.Handle(rg.Get(common.SettingsEndpoint, common.TabEndpoint, common.UsageEndpoint), privateReadChain.Then(s.handler(s.getUsageSettings)))
	router.Handle(rg.Post(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint, common.NewEndpoint), privateWriteChain.Then(s.handler(s.postAPIKeySettings)))
	router.Handle(rg.Get(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint), privateReadChain.Then(s.handler(s.getBillingSettings)))
	router.Handle(rg.Post(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint, common.PreviewEndpoint), privateWriteChain.Then(s.handler(s.postBillingPreview)))
	router.Handle(rg.Put(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint), subscribedWrite.Then(s.handler(s.putBilling)))
	router.Handle(rg.Get(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint, common.CancelEndpoint), subscribedRead.ThenFunc(s.getCancelSubscription))
	router.Handle(rg.Get(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint, common.UpdateEndpoint), subscribedRead.ThenFunc(s.getUpdateSubscription))
	router.Handle(rg.Get(common.UserEndpoint, common.StatsEndpoint), privateFreshReadChain.ThenFunc(s.getAccountStats))
	router.Handle(rg.Delete(common.APIKeysEndpoint, arg(common.ParamKey)), privateWriteChain.ThenFunc(s.deleteAPIKey))
	router.Handle(rg.Delete(common.UserEndpoint), privateWriteChain.ThenFunc(s.deleteAccount))
	router.Handle(rg.Get(common.SupportEndpoint), privateReadChain.Then(s.handler(s.getSupport)))
	router.Handle(rg.Post(common.SupportEndpoint), privateWriteChain.Then(s.handler(s.postSupport)))
	router.Handle(rg.Delete(common.NotificationEndpoint, arg(common.ParamID)), openWrite.Append(s.private).ThenFunc(s.dismissNotification))
	router.Handle(rg.Get("robots.txt"), publicChain.ThenFunc(portalRobotsTXT))
	router.Handle(http.MethodGet+" "+prefix+"{$}", privateReadChain.ThenFunc(s.getPortal))
	// wildcard
	router.Handle(http.MethodGet+" "+prefix+"{path...}", publicChain.ThenFunc(s.notFound))
}

func (s *Server) isMaintenanceMode() bool {
	return s.maintenanceMode.Load()
}

func (s *Server) handler(modelFunc ModelFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// such composition makes business logic and rendering testable separately
		renderCtx, tpl, err := modelFunc(w, r)
		if err != nil {
			switch err {
			case errInvalidSession:
				common.Redirect(s.relURL(common.LoginEndpoint), w, r)
			case errInvalidPathArg, errInvalidRequestArg:
				s.redirectError(http.StatusBadRequest, w, r)
			case db.ErrMaintenance:
				s.redirectError(http.StatusServiceUnavailable, w, r)
			case errRegistrationDisabled:
				s.redirectError(http.StatusNotFound, w, r)
			default:
				slog.ErrorContext(ctx, "Failed to create model for request", common.ErrAttr(err))
				s.redirectError(http.StatusInternalServerError, w, r)
			}
			return
		}

		s.render(w, r, tpl, renderCtx)
	})
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
		slog.ErrorContext(ctx, "Failed to render template", "name", name, common.ErrAttr(err))
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
		CDN:         s.CDNURL,
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
	code, _ := strconv.Atoi(r.PathValue(common.ParamCode))
	if (code < 100) || (code > 600) {
		slog.ErrorContext(r.Context(), "Invalid error code", "code", code)
		code = http.StatusInternalServerError
	}

	s.renderError(r.Context(), w, code)
}

func (s *Server) redirectError(code int, w http.ResponseWriter, r *http.Request) {
	url := s.relURL(common.ErrorEndpoint + "/" + strconv.Itoa(code))
	common.Redirect(url, w, r)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(r.Context(), w, http.StatusNotFound)
}

func (s *Server) maintenance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isMaintenanceMode() {
			slog.Log(r.Context(), common.LevelTrace, "Rejecting request under maintenance mode")
			s.redirectError(http.StatusServiceUnavailable, w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) private(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
}
