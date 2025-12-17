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
	"github.com/rs/xid"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	errInvalidPathArg      = errors.New("path argument is not valid")
	ErrInvalidRequestArg   = errors.New("request argument is not valid")
	errOrgSoftDeleted      = errors.New("organization is deleted")
	errPropertySoftDeleted = errors.New("property is deleted")
	errLimitedFeature      = errors.New("feature is limited")

	englishCaser = cases.Title(language.English)
)

const (
	PortalService = "portal"
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
type ViewModel struct {
	Model      Model
	View       string
	AuditEvent *common.AuditLogEvent
}
type ViewModelHandler func(http.ResponseWriter, *http.Request) (*ViewModel, error)
type AuditLogsConstructor func(context.Context, *dbgen.User, int, int) (*MainAuditLogsRenderContext, error)

type RequestContext struct {
	Path        string
	LoggedIn    bool
	CurrentYear int
	UserName    string
	UserEmail   string
	CDN         string
}

type PaginationRenderContext struct {
	From    int
	To      int
	Count   int
	Page    int
	PerPage int
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
	GitCommit  string
	Enterprise bool
}

func (ac *AlertRenderContext) ClearAlerts() {
	ac.ErrorMessage = ""
	ac.SuccessMessage = ""
	ac.WarningMessage = ""
	ac.InfoMessage = ""
}

type Server struct {
	Store              db.Implementor
	TimeSeries         common.TimeSeriesStore
	APIURL             string
	CDNURL             string
	Prefix             string
	IDHasher           common.IdentifierHasher
	template           *Templates
	XSRF               *common.XSRFMiddleware
	Sessions           *session.Manager
	Mailer             common.Mailer
	Stage              string
	PlanService        billing.PlanService
	PuzzleEngine       puzzle.Engine
	Metrics            common.PortalMetrics
	maintenanceMode    atomic.Bool
	canRegister        atomic.Bool
	SettingsTabs       []*SettingsTab
	RateLimiter        ratelimit.HTTPRateLimiter
	RenderConstants    interface{}
	Jobs               Jobs
	PlatformCtx        interface{}
	DataCtx            interface{}
	CountryCodeHeader  common.ConfigItem
	UserLimiter        api.UserLimiter
	AuditLogsFunc      AuditLogsConstructor
	SubscriptionLimits db.SubscriptionLimits
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

func (s *Server) Init(ctx context.Context, templateBuilder *TemplatesBuilder, gitCommit string, sessionPersistInterval time.Duration) error {
	prefix := common.RelURL(s.Prefix, "/")

	templateBuilder.AddFunctions(ctx, funcMap(prefix))

	var err error
	s.template, err = templateBuilder.Build(ctx)
	if err != nil {
		return err
	}

	s.Sessions.Init(PortalService, prefix, sessionPersistInterval)

	s.Jobs = s
	s.SettingsTabs = s.createSettingsTabs()
	s.RenderConstants = NewRenderConstants()
	s.AuditLogsFunc = s.CreateAuditLogsContext

	platformCtx := &PlatformRenderContext{
		GitCommit:  gitCommit,
		Enterprise: s.isEnterprise(),
	}
	if len(gitCommit) == 0 {
		platformCtx.GitCommit = xid.New().String()
	}

	s.PlatformCtx = platformCtx

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

func (s *Server) Setup(domain string, security alice.Constructor) *common.RouteGenerator {
	prefix := domain + s.RelURL("/")
	rg := &common.RouteGenerator{Prefix: prefix}
	slog.Debug("Setting up the portal routes", "prefix", prefix, "enterprise", s.isEnterprise())
	s.setupWithPrefix(rg, security)
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

func defaultMaxBytesHandler(next http.Handler) http.Handler {
	return http.MaxBytesHandler(next, 256*1024)
}

func (s *Server) MiddlewarePublicChain(rg *common.RouteGenerator, security alice.Constructor) alice.Chain {
	const (
		// by default we are allowing 1 request per 2 seconds from a single client IP address with a {leakyBucketCap} burst
		// for portal we raise these limits for authenticated users and for CDN we have full-on caching
		defaultLeakyBucketCap = 10
		defaultLeakInterval   = 2 * time.Second
	)

	ratelimiter := s.RateLimiter.RateLimitExFunc(defaultLeakyBucketCap, defaultLeakInterval)
	svc := common.ServiceMiddleware(PortalService)
	cop := http.NewCrossOriginProtection()

	return alice.New(svc, common.Recovered, security, s.Metrics.HandlerIDFunc(rg.LastPath), ratelimiter, cop.Handler, monitoring.Logged)
}

func (s *Server) MiddlewarePrivateRead(public alice.Chain) alice.Chain {
	internalTimeout := common.TimeoutHandler(10 * time.Second)
	return public.Append(s.maintenance, internalTimeout, s.private)
}

func (s *Server) MiddlewarePrivateWrite(public alice.Chain) alice.Chain {
	internalTimeout := common.TimeoutHandler(10 * time.Second)
	return public.Append(s.maintenance, defaultMaxBytesHandler, internalTimeout, s.csrf(s.csrfUserIDKeyFunc), s.private)
}

func (s *Server) setupWithPrefix(rg *common.RouteGenerator, security alice.Constructor) {
	arg := func(s string) string {
		return fmt.Sprintf("{%s}", s)
	}

	// NOTE: with regards to CORS, for portal server we want CORS to be before rate limiting

	// separately configured "public" ones
	public := s.MiddlewarePublicChain(rg, security)
	publicTimeout := common.TimeoutHandler(2 * time.Second)
	openRead := public.Append(s.maintenance, publicTimeout)
	rg.Handle(rg.Get(common.LoginEndpoint), openRead.Append(common.Cached), s.Handler(s.getLogin))
	rg.Handle(rg.Get(common.RegisterEndpoint), openRead.Append(common.Cached), s.Handler(s.getRegister))
	rg.Handle(rg.Get(common.ErrorEndpoint, arg(common.ParamCode)), public, http.HandlerFunc(s.error))
	rg.Handle(rg.Get(common.ExpiredEndpoint), public, http.HandlerFunc(s.expired))
	rg.Handle(rg.Get(common.LogoutEndpoint), public, http.HandlerFunc(s.logout))

	// openWrite is protected by captcha, other "write" handlers are protected by CSRF token / auth
	openWrite := public.Append(s.maintenance, defaultMaxBytesHandler, publicTimeout)
	csrfEmail := openWrite.Append(s.csrf(s.csrfUserEmailKeyFunc))
	privateWrite := s.MiddlewarePrivateWrite(public)
	privateRead := s.MiddlewarePrivateRead(public)

	rg.Handle(rg.Post(common.LoginEndpoint), openWrite, http.HandlerFunc(s.postLogin))
	rg.Handle(rg.Post(common.RegisterEndpoint), openWrite, http.HandlerFunc(s.postRegister))
	rg.Handle(rg.Post(common.TwoFactorEndpoint), csrfEmail, http.HandlerFunc(s.postTwoFactor))
	rg.Handle(rg.Post(common.ResendEndpoint), csrfEmail, http.HandlerFunc(s.resend2fa))
	rg.Handle(rg.Get(common.OrgEndpoint, common.NewEndpoint), privateRead, s.Handler(s.getNewOrg))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg)), privateRead, http.HandlerFunc(s.getPortal))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.DashboardEndpoint), privateRead, s.Handler(s.getOrgDashboard))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.MembersEndpoint), privateRead, s.Handler(s.getOrgMembers))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.SettingsEndpoint), privateRead, s.Handler(s.getOrgSettings))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.EventsEndpoint), privateRead, s.Handler(s.getOrgAuditLogs))
	rg.Handle(rg.Put(common.OrgEndpoint, arg(common.ParamOrg), common.EditEndpoint), privateWrite, s.Handler(s.putOrg))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertiesEndpoint), privateRead, s.Handler(s.getOrgProperties))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, common.NewEndpoint), privateRead, s.Handler(s.getNewOrgProperty))
	rg.Handle(rg.Post(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, common.NewEndpoint), privateWrite, http.HandlerFunc(s.postNewOrgProperty))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty)), privateRead, s.Handler(s.getPropertyDashboard))
	rg.Handle(rg.Put(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.EditEndpoint), privateWrite, s.Handler(s.putProperty))
	rg.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.DeleteEndpoint), privateWrite, http.HandlerFunc(s.deleteProperty))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.ReportsEndpoint), privateRead, s.Handler(s.getPropertyReportsTab))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.SettingsEndpoint), privateRead, s.Handler(s.getPropertySettingsTab))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.IntegrationsEndpoint), privateRead, s.Handler(s.getPropertyIntegrationsTab))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.EventsEndpoint), privateRead, s.Handler(s.getPropertyAuditLogsTab))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.StatsEndpoint, arg(common.ParamPeriod)), privateRead, http.HandlerFunc(s.getPropertyStats))

	rg.Handle(rg.Get(common.SettingsEndpoint), privateRead, s.Handler(s.getSettings))
	rg.Handle(rg.Get(common.SettingsEndpoint, common.TabEndpoint, arg(common.ParamTab)), privateRead, s.Handler(s.getSettingsTab))
	rg.Handle(rg.Post(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint, common.EmailEndpoint), privateWrite, s.Handler(s.editEmail))
	rg.Handle(rg.Put(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), privateWrite, s.Handler(s.putGeneralSettings))
	rg.Handle(rg.Post(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint, common.NewEndpoint), privateWrite, s.Handler(s.postAPIKeySettings))

	rg.Handle(rg.Get(common.AuditLogsEndpoint), privateRead, s.Handler(s.getAuditLogs))

	rg.Handle(rg.Get(common.UserEndpoint, common.StatsEndpoint), privateRead, http.HandlerFunc(s.getAccountStats))
	rg.Handle(rg.Post(common.APIKeysEndpoint, arg(common.ParamKey)), privateWrite, s.Handler(s.rotateAPIKey))
	rg.Handle(rg.Delete(common.APIKeysEndpoint, arg(common.ParamKey)), privateWrite, http.HandlerFunc(s.deleteAPIKey))
	rg.Handle(rg.Delete(common.UserEndpoint), privateWrite, http.HandlerFunc(s.deleteAccount))
	rg.Handle(rg.Delete(common.NotificationEndpoint, arg(common.ParamID)), openWrite.Append(s.private), http.HandlerFunc(s.dismissNotification))
	rg.Handle(rg.Post(common.ErrorEndpoint), privateRead, http.HandlerFunc(s.postClientSideError))
	rg.Handle(rg.Get(common.EchoPuzzleEndpoint, arg(common.ParamDifficulty)), privateRead, http.HandlerFunc(s.echoPuzzle))

	s.setupEnterprise(rg, privateRead, privateWrite)

	// {$} matches the end of the URL
	rg.Handle(http.MethodGet+" "+rg.Prefix+"{$}", privateRead, http.HandlerFunc(s.getPortal))
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

func (s *Server) Handler(modelFunc ViewModelHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// such composition makes business logic and rendering testable separately
		mv, err := modelFunc(w, r)
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
			case errLimitedFeature:
				s.RedirectError(http.StatusPaymentRequired, w, r)
			case context.DeadlineExceeded:
				slog.WarnContext(ctx, "Context deadline exceeded during model function", common.ErrAttr(err))
			default:
				slog.ErrorContext(ctx, "Failed to create model for request", common.ErrAttr(err))
				s.RedirectError(http.StatusInternalServerError, w, r)
			}
			return
		}
		// If tpl is not empty, render the template with the model.
		if mv.View != "" {
			s.render(w, r, mv.View, mv.Model)
		}
		// If tpl is empty, it means modelFunc handled the response (e.g., redirect, error, or manual write).
		if mv.AuditEvent != nil {
			s.Store.AuditLog().RecordEvent(ctx, mv.AuditEvent, common.AuditLogSourcePortal)
		}
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
	const (
		// "authenticated" means when we "legitimize" IP address using business logic
		authenticatedBucketCap = 20
		// this effectively means 1 request/second
		authenticatedLeakInterval = 1 * time.Second
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := s.Sessions.SessionStart(w, r)

		ctx := r.Context()
		ctx = context.WithValue(ctx, common.SessionIDContextKey, sess.ID())

		if step, ok := sess.Get(ctx, session.KeyLoginStep).(int); ok {
			// this is a sign it could be a local stale session in case user finished login on another node
			if (step == loginStepSignInVerify) || (step == loginStepSignUpVerify) {
				slog.WarnContext(ctx, "About to recover potential stale session from DB")
				s.Sessions.RecoverSession(ctx, sess)
				step, _ = sess.Get(ctx, session.KeyLoginStep).(int)
			}

			if step == loginStepCompleted {
				// update limits each time as rate limiting gets cleaned up frequently (impact shouldn't be much in portal)
				s.RateLimiter.UpdateRequestLimits(r, authenticatedBucketCap, authenticatedLeakInterval)

				ctx = context.WithValue(ctx, common.LoggedInContextKey, true)
				ctx = context.WithValue(ctx, common.SessionContextKey, sess)

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			} else {
				slog.WarnContext(ctx, "Session present, but login not finished", "step", step)
			}
		}

		// for HTMX requests we don't want to do it (as they are mostly "background")
		if _, ok := r.Header[common.HeaderHtmxRequest]; !ok {
			_ = sess.Set(session.KeyReturnURL, r.URL.RequestURI())
		}

		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
	})
}
