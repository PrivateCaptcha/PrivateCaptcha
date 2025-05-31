package portal

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/justinas/alice"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	errInvalidPathArg      = errors.New("path argument is not valid")
	ErrInvalidRequestArg   = errors.New("request argument is not valid")
	errOrgSoftDeleted      = errors.New("organization is deleted")
	errPropertySoftDeleted = errors.New("property is deleted")
)

func funcMap(prefix string) template.FuncMap {
	return template.FuncMap{
		"relURL": func(s string) any {
			return common.RelURL(prefix, s)
		},
		"partsURL": func(a ...string) any {
			return common.RelURL(prefix, strings.Join(a, "/"))
		},
	}
}

type CsrfKeyFunc func(http.ResponseWriter, *http.Request) string

type Model = any
type ModelFunc func(http.ResponseWriter, *http.Request) (Model, string, error)

type RequestContext struct {
	Path        string
	LoggedIn    bool
	CurrentYear int
	UserName    string
	UserEmail   string
	CDN         string
}

type CsrfRenderContext struct {
	Token string
}

type systemNotificationContext struct {
	Notification   string
	NotificationID string
}

type AlertRenderContext struct {
	ErrorMessage   string
	SuccessMessage string
	WarningMessage string
	InfoMessage    string
}

type CaptchaRenderContext struct {
	CaptchaError         string
	CaptchaEndpoint      string
	CaptchaSolutionField string
	CaptchaSitekey       string
	CaptchaDebug         bool
}

type PlatformRenderContext struct {
	Enterprise bool
}

func (ac *AlertRenderContext) ClearAlerts() {
	ac.ErrorMessage = ""
	ac.SuccessMessage = ""
	ac.WarningMessage = ""
	ac.InfoMessage = ""
}

type Server struct {
	Store           db.Implementor
	TimeSeries      common.TimeSeriesStore
	APIURL          string
	CDNURL          string
	Prefix          string
	template        *Templates
	XSRF            *common.XSRFMiddleware
	Sessions        *session.Manager
	Mailer          common.Mailer
	Stage           string
	PlanService     billing.PlanService
	PuzzleEngine    puzzle.Engine
	Metrics         common.Metrics
	maintenanceMode atomic.Bool
	canRegister     atomic.Bool
	SettingsTabs    []*SettingsTab
	Auth            *AuthMiddleware
	RenderConstants interface{}
	Jobs            Jobs
	PlatformCtx     interface{}
}

func (s *Server) createSettingsTabs() []*SettingsTab {
	return []*SettingsTab{
		{
			ID:             common.GeneralEndpoint,
			Name:           "General",
			TemplatePrefix: settingsGeneralTemplatePrefix,
			ModelHandler:   s.getGeneralSettings,
		},
		{
			ID:             common.APIKeysEndpoint,
			Name:           "API Keys",
			TemplatePrefix: settingsAPIKeysTemplatePrefix,
			ModelHandler:   s.getAPIKeysSettings,
		},
		{
			ID:             common.UsageEndpoint,
			Name:           "Usage",
			TemplatePrefix: settingsUsageTemplatePrefix,
			ModelHandler:   s.getUsageSettings,
		},
	}
}

func (s *Server) Init(ctx context.Context, templateBuilder *TemplatesBuilder) error {
	prefix := common.RelURL(s.Prefix, "/")

	templateBuilder.AddFunctions(ctx, funcMap(prefix))

	var err error
	s.template, err = templateBuilder.Build(ctx)
	if err != nil {
		return err
	}

	s.Sessions.Path = prefix

	s.Jobs = s
	s.SettingsTabs = s.createSettingsTabs()
	s.RenderConstants = NewRenderConstants()
	s.PlatformCtx = &PlatformRenderContext{
		Enterprise: s.isEnterprise(),
	}

	return nil
}

func (s *Server) UpdateConfig(ctx context.Context, cfg common.ConfigStore) {
	maintenanceMode := config.AsBool(cfg.Get(common.MaintenanceModeKey))
	oldMaintenanceMode := s.maintenanceMode.Swap(maintenanceMode)

	registrationAllowed := config.AsBool(cfg.Get(common.RegistrationAllowedKey))
	s.canRegister.Store(registrationAllowed)

	if oldMaintenanceMode != maintenanceMode {
		slog.InfoContext(ctx, "Maintenance mode change", "old", oldMaintenanceMode, "new", maintenanceMode)
	}
}

func (s *Server) Setup(router *http.ServeMux, domain string, security alice.Constructor) *RouteGenerator {
	prefix := domain + s.RelURL("/")
	rg := &RouteGenerator{Prefix: prefix}
	slog.Debug("Setting up the portal routes", "prefix", prefix)
	s.setupWithPrefix(router, rg, security)
	return rg
}

func (s *Server) SetupCatchAll(router *http.ServeMux, domain string, chain alice.Chain) {
	prefix := domain + s.RelURL("/")
	slog.Debug("Setting up the catchall portal routes", "prefix", prefix)
	s.setupCatchAllWithPrefix(router, prefix, chain)
}

func (s *Server) RelURL(url string) string {
	return common.RelURL(s.Prefix, url)
}

func (s *Server) PartsURL(a ...string) string {
	return s.RelURL(strings.Join(a, "/"))
}

// RouteGenerator's point is to passthrough the path correctly to the std.Handler() of slok/go-http-metrics
// the whole magic can break if for some reason Go will not evaluate result of Route() before calling Alice's Then()
// when calling router.Handle() in setupWithPrefix()
type RouteGenerator struct {
	Prefix string
	Path   string
}

func (rg *RouteGenerator) Route(method string, parts ...string) string {
	rg.Path = rg.Prefix + strings.Join(parts, "/")
	return method + " " + rg.Path
}

func (rg *RouteGenerator) Get(parts ...string) string {
	return rg.Route(http.MethodGet, parts...)
}

func (rg *RouteGenerator) Post(parts ...string) string {
	return rg.Route(http.MethodPost, parts...)
}

func (rg *RouteGenerator) Put(parts ...string) string {
	return rg.Route(http.MethodPut, parts...)
}

func (rg *RouteGenerator) Delete(parts ...string) string {
	return rg.Route(http.MethodDelete, parts...)
}

func (rg *RouteGenerator) LastPath() string {
	result := rg.Path
	// side-effect: this will cause go http metrics handler to use handlerID based on request Path
	rg.Path = ""
	return result
}

func defaultMaxBytesHandler(next http.Handler) http.Handler {
	return http.MaxBytesHandler(next, 256*1024)
}

func (s *Server) MiddlewarePublicChain(rg *RouteGenerator, security alice.Constructor) alice.Chain {
	return alice.New(common.Recovered, security, s.Metrics.HandlerFunc(rg.LastPath), s.Auth.RateLimit(), monitoring.Logged)
}

func (s *Server) MiddlewarePrivateRead(public alice.Chain) alice.Chain {
	internalTimeout := common.TimeoutHandler(10 * time.Second)
	return public.Append(s.maintenance, internalTimeout, s.private)
}

func (s *Server) MiddlewarePrivateWrite(public alice.Chain) alice.Chain {
	internalTimeout := common.TimeoutHandler(10 * time.Second)
	return public.Append(s.maintenance, defaultMaxBytesHandler, internalTimeout, s.csrf(s.csrfUserIDKeyFunc), s.private)
}

func (s *Server) setupWithPrefix(router *http.ServeMux, rg *RouteGenerator, security alice.Constructor) {
	arg := func(s string) string {
		return fmt.Sprintf("{%s}", s)
	}

	// NOTE: with regards to CORS, for portal server we want CORS to be before rate limiting

	// separately configured "public" ones
	public := s.MiddlewarePublicChain(rg, security)
	publicTimeout := common.TimeoutHandler(2 * time.Second)
	openRead := public.Append(s.maintenance, publicTimeout)
	router.Handle(rg.Get(common.LoginEndpoint), openRead.Then(common.Cached(s.Handler(s.getLogin))))
	router.Handle(rg.Get(common.RegisterEndpoint), openRead.Then(common.Cached(s.Handler(s.getRegister))))
	router.Handle(rg.Get(common.TwoFactorEndpoint), openRead.ThenFunc(s.getTwoFactor))
	router.Handle(rg.Get(common.ErrorEndpoint, arg(common.ParamCode)), public.ThenFunc(s.error))
	router.Handle(rg.Get(common.ExpiredEndpoint), public.ThenFunc(s.expired))
	router.Handle(rg.Get(common.LogoutEndpoint), public.ThenFunc(s.logout))

	// openWrite is protected by captcha, other "write" handlers are protected by CSRF token / auth
	openWrite := public.Append(s.maintenance, defaultMaxBytesHandler, publicTimeout)
	csrfEmail := openWrite.Append(s.csrf(s.csrfUserEmailKeyFunc))
	privateWrite := s.MiddlewarePrivateWrite(public)
	privateRead := s.MiddlewarePrivateRead(public)

	router.Handle(rg.Post(common.LoginEndpoint), openWrite.ThenFunc(s.postLogin))
	router.Handle(rg.Post(common.RegisterEndpoint), openWrite.ThenFunc(s.postRegister))
	router.Handle(rg.Post(common.TwoFactorEndpoint), csrfEmail.ThenFunc(s.postTwoFactor))
	router.Handle(rg.Post(common.ResendEndpoint), csrfEmail.ThenFunc(s.resend2fa))
	router.Handle(rg.Get(common.OrgEndpoint, common.NewEndpoint), privateRead.Then(s.Handler(s.getNewOrg)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg)), privateRead.ThenFunc(s.getPortal))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.DashboardEndpoint), privateRead.Then(s.Handler(s.getOrgDashboard)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.MembersEndpoint), privateRead.Then(s.Handler(s.getOrgMembers)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.SettingsEndpoint), privateRead.Then(s.Handler(s.getOrgSettings)))
	router.Handle(rg.Put(common.OrgEndpoint, arg(common.ParamOrg), common.EditEndpoint), privateWrite.Then(s.Handler(s.putOrg)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, common.NewEndpoint), privateRead.Then(s.Handler(s.getNewOrgProperty)))
	router.Handle(rg.Post(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, common.NewEndpoint), privateWrite.ThenFunc(s.postNewOrgProperty))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty)), privateRead.Then(s.Handler(s.getPropertyDashboard)))
	router.Handle(rg.Put(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.EditEndpoint), privateWrite.Then(s.Handler(s.putProperty)))
	router.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.DeleteEndpoint), privateWrite.ThenFunc(s.deleteProperty))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.ReportsEndpoint), privateRead.Then(s.Handler(s.getPropertyReportsTab)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.SettingsEndpoint), privateRead.Then(s.Handler(s.getPropertySettingsTab)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.IntegrationsEndpoint), privateRead.Then(s.Handler(s.getPropertyIntegrationsTab)))
	router.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.StatsEndpoint, arg(common.ParamPeriod)), privateRead.ThenFunc(s.getPropertyStats))

	router.Handle(rg.Get(common.SettingsEndpoint), privateRead.Then(s.Handler(s.getSettings)))
	router.Handle(rg.Get(common.SettingsEndpoint, common.TabEndpoint, arg(common.ParamTab)), privateRead.Then(s.Handler(s.getSettingsTab)))
	router.Handle(rg.Post(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint, common.EmailEndpoint), privateWrite.Then(s.Handler(s.editEmail)))
	router.Handle(rg.Put(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), privateWrite.Then(s.Handler(s.putGeneralSettings)))
	router.Handle(rg.Post(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint, common.NewEndpoint), privateWrite.Then(s.Handler(s.postAPIKeySettings)))

	router.Handle(rg.Get(common.UserEndpoint, common.StatsEndpoint), privateRead.ThenFunc(s.getAccountStats))
	router.Handle(rg.Delete(common.APIKeysEndpoint, arg(common.ParamKey)), privateWrite.ThenFunc(s.deleteAPIKey))
	router.Handle(rg.Delete(common.UserEndpoint), privateWrite.ThenFunc(s.deleteAccount))
	router.Handle(rg.Delete(common.NotificationEndpoint, arg(common.ParamID)), openWrite.Append(s.private).ThenFunc(s.dismissNotification))
	router.Handle(rg.Post(common.ErrorEndpoint), privateRead.ThenFunc(s.postClientSideError))
	router.Handle(rg.Get(common.EchoPuzzleEndpoint, arg(common.ParamDifficulty)), privateRead.ThenFunc(s.echoPuzzle))

	s.setupEnterprise(router, rg, privateWrite)

	// {$} matches the end of the URL
	router.Handle(http.MethodGet+" "+rg.Prefix+"{$}", privateRead.ThenFunc(s.getPortal))
}

func (s *Server) setupCatchAllWithPrefix(router *http.ServeMux, prefix string, chain alice.Chain) {
	// wildcards (everything not matched will be handled in main())
	router.Handle(http.MethodGet+" "+prefix+common.OrgEndpoint+"/", chain.ThenFunc(s.notFound))
	router.Handle(http.MethodGet+" "+prefix+common.ErrorEndpoint+"/", chain.ThenFunc(s.notFound))
	router.Handle(http.MethodGet+" "+prefix+common.SettingsEndpoint+"/", chain.ThenFunc(s.notFound))
	router.Handle(http.MethodGet+" "+prefix+common.UserEndpoint+"/", chain.ThenFunc(s.notFound))
}

func (s *Server) isMaintenanceMode() bool {
	return s.maintenanceMode.Load()
}

func (s *Server) Shutdown() {
	s.Auth.Shutdown()
}

func (s *Server) Handler(modelFunc ModelFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// such composition makes business logic and rendering testable separately
		renderCtx, tpl, err := modelFunc(w, r)
		if err != nil {
			switch err {
			case errInvalidSession:
				common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
			case errInvalidPathArg, ErrInvalidRequestArg:
				s.RedirectError(http.StatusBadRequest, w, r)
			case errOrgSoftDeleted:
				common.Redirect(s.RelURL("/"), http.StatusBadRequest, w, r)
			case errPropertySoftDeleted:
				if orgID, err := s.OrgID(r); err == nil {
					url := s.RelURL(fmt.Sprintf("/%s/%v", common.OrgEndpoint, orgID))
					common.Redirect(url, http.StatusBadRequest, w, r)
				} else {
					common.Redirect(s.RelURL("/"), http.StatusBadRequest, w, r)
				}
			case db.ErrPermissions:
				s.RedirectError(http.StatusForbidden, w, r)
			case db.ErrSoftDeleted:
				s.RedirectError(http.StatusNotAcceptable, w, r)
			case db.ErrMaintenance:
				s.RedirectError(http.StatusServiceUnavailable, w, r)
			case errRegistrationDisabled:
				s.RedirectError(http.StatusNotFound, w, r)
			case context.DeadlineExceeded:
				slog.WarnContext(ctx, "Context deadline exceeded during model function", common.ErrAttr(err))
			default:
				slog.ErrorContext(ctx, "Failed to create model for request", common.ErrAttr(err))
				s.RedirectError(http.StatusInternalServerError, w, r)
			}
			return
		}
		// If tpl is not empty, render the template with the model.
		if tpl != "" {
			s.render(w, r, tpl, renderCtx)
		}
		// If tpl is empty, it means modelFunc handled the response (e.g., redirect, error, or manual write).
	})
}

func (s *Server) maintenance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isMaintenanceMode() {
			slog.Log(r.Context(), common.LevelTrace, "Rejecting request under maintenance mode")
			s.RedirectError(http.StatusServiceUnavailable, w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) private(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := s.Sessions.SessionStart(w, r)

		ctx := r.Context()
		ctx = context.WithValue(ctx, common.SessionIDContextKey, sess.SessionID())

		if step, ok := sess.Get(session.KeyLoginStep).(int); ok {
			if step == loginStepCompleted {
				// update limits each time as rate limiting gets cleaned up frequently (impact shouldn't be much in portal)
				s.Auth.UpdateLimits(r)

				ctx = context.WithValue(ctx, common.LoggedInContextKey, true)
				ctx = context.WithValue(ctx, common.SessionContextKey, sess)

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			} else {
				slog.WarnContext(ctx, "Session present, but login not finished", "step", step)
			}
		}

		_ = sess.Set(session.KeyReturnURL, r.URL.RequestURI())
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
	})
}
