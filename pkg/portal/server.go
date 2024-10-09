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
	"time"

	"github.com/rs/cors"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
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

type Model = any
type ModelFunc func(http.ResponseWriter, *http.Request) (Model, string, error)

type requestContext struct {
	Path        string
	LoggedIn    bool
	CurrentYear int
	UserName    string
	UserEmail   string
}

type csrfRenderContext struct {
	Token string
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
	Store      *db.BusinessStore
	TimeSeries *db.TimeSeriesStore
	Domain     string
	Prefix     string
	template   *web.Template
	XSRF       XSRFMiddleware
	Session    session.Manager
	Mailer     common.Mailer
	Stage      string
	PaddleAPI  billing.PaddleAPI
	cors       *cors.Cors
	ApiRelURL  string
	Verifier   puzzle.Verifier
}

func (s *Server) Init() {
	prefix := common.RelURL(s.Prefix, "/")
	s.template = web.NewTemplates(funcMap(prefix))
	s.Session.Path = prefix
}

func (s *Server) Setup(router *http.ServeMux, ratelimiter common.Middleware) {
	corsDomain := s.Domain
	if len(corsDomain) == 0 {
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
	corsHandler := common.HandlerWrapper(s.cors.Handler)

	s.setupWithPrefix(s.Domain+s.relURL("/"), router, ratelimiter, corsHandler)
}

func (s *Server) relURL(url string) string {
	return common.RelURL(s.Prefix, url)
}

func (s *Server) partsURL(a ...string) string {
	return s.relURL(strings.Join(a, "/"))
}

func (s *Server) setupWithPrefix(prefix string, router *http.ServeMux, ratelimiter, corsHandler common.Middleware) {
	slog.Debug("Setting up the portal routes", "prefix", prefix)

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

	arg := func(s string) string {
		return fmt.Sprintf("{%s}", s)
	}

	maxBytesHandler := func(next http.HandlerFunc) http.HandlerFunc {
		return common.MaxBytesHandler(next, 256*1024)
	}

	// NOTE: with regards to CORS, for portal server we want CORS to be before rate limiting

	// separately configured "public" ones
	router.HandleFunc(get(common.LoginEndpoint), corsHandler(ratelimiter(s.handler(s.getLogin))))
	router.HandleFunc(get(common.RegisterEndpoint), corsHandler(ratelimiter(s.handler(s.getRegister))))
	router.HandleFunc(get(common.TwoFactorEndpoint), corsHandler(ratelimiter(s.getTwoFactor)))
	router.HandleFunc(get(common.ErrorEndpoint, arg(common.ParamCode)), corsHandler(ratelimiter(common.CacheControl(s.error))))
	router.HandleFunc(get(common.ExpiredEndpoint), corsHandler(ratelimiter(common.CacheControl(s.expired))))
	router.HandleFunc(get(common.LogoutEndpoint), corsHandler(ratelimiter(s.logout)))

	// configured with middlewares
	openWrite := common.NewMiddleWareChain(common.Recovered, corsHandler, ratelimiter, common.Logged, maxBytesHandler)
	writeChain := openWrite.Add(s.csrf)
	privateWriteChain := writeChain.Add(s.private)
	subscribedWrite := privateWriteChain.Add(s.subscribed)

	privateReadChain := common.NewMiddleWareChain(common.Recovered, corsHandler, ratelimiter, s.private)
	subscribedRead := privateReadChain.Add(s.subscribed)

	router.HandleFunc(post(common.LoginEndpoint), openWrite.Build(s.postLogin))
	router.HandleFunc(post(common.RegisterEndpoint), openWrite.Build(s.postRegister))
	router.HandleFunc(post(common.TwoFactorEndpoint), writeChain.Build(s.postTwoFactor))
	router.HandleFunc(post(common.ResendEndpoint), writeChain.Build(s.resend2fa))
	router.HandleFunc(get(common.OrgEndpoint, common.NewEndpoint), privateReadChain.Build(s.handler(s.getNewOrg)))
	router.HandleFunc(post(common.OrgEndpoint, common.NewEndpoint), privateWriteChain.Build(s.postNewOrg))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg)), privateReadChain.Build(s.getPortal))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.DashboardEndpoint), privateReadChain.Build(s.handler(s.getOrgDashboard)))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.MembersEndpoint), privateReadChain.Build(s.handler(s.getOrgMembers)))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.TabEndpoint, common.SettingsEndpoint), privateReadChain.Build(s.handler(s.getOrgSettings)))
	router.HandleFunc(post(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWriteChain.Build(s.handler(s.postOrgMembers)))
	router.HandleFunc(delete(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint, arg(common.ParamUser)), privateWriteChain.Build(s.deleteOrgMembers))
	router.HandleFunc(put(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWriteChain.Build(s.joinOrg))
	router.HandleFunc(delete(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWriteChain.Build(s.leaveOrg))
	router.HandleFunc(put(common.OrgEndpoint, arg(common.ParamOrg), common.EditEndpoint), privateWriteChain.Build(s.handler(s.putOrg)))
	router.HandleFunc(delete(common.OrgEndpoint, arg(common.ParamOrg), common.DeleteEndpoint), privateWriteChain.Build(s.deleteOrg))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, common.NewEndpoint), subscribedRead.Build(s.handler(s.getNewOrgProperty)))
	router.HandleFunc(post(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, common.NewEndpoint), subscribedWrite.Build(s.postNewOrgProperty))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty)), privateReadChain.Build(s.handler(s.getPropertyDashboard)))
	router.HandleFunc(put(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.EditEndpoint), privateWriteChain.Build(s.handler(s.putProperty)))
	router.HandleFunc(delete(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.DeleteEndpoint), privateWriteChain.Build(s.deleteProperty))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.ReportsEndpoint), privateReadChain.Build(s.handler(s.getPropertyReports)))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.SettingsEndpoint), privateReadChain.Build(s.handler(s.getPropertySettings)))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.TabEndpoint, common.IntegrationsEndpoint), privateReadChain.Build(s.handler(s.getPropertyIntegrations)))
	router.HandleFunc(get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.StatsEndpoint, arg(common.ParamPeriod)), privateReadChain.Build(common.NoCache(s.getPropertyStats)))
	router.HandleFunc(get(common.SettingsEndpoint), privateReadChain.Build(s.handler(s.getSettings)))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), privateReadChain.Build(s.handler(s.getGeneralSettings)))
	router.HandleFunc(post(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint, common.EmailEndpoint), privateWriteChain.Build(s.handler(s.editEmail)))
	router.HandleFunc(put(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint), privateWriteChain.Build(s.handler(s.putGeneralSettings)))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint), privateReadChain.Build(s.handler(s.getAPIKeysSettings)))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.UsageEndpoint), privateReadChain.Build(s.handler(s.getUsageSettings)))
	router.HandleFunc(post(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint, common.NewEndpoint), privateWriteChain.Build(s.handler(s.postAPIKeySettings)))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint), privateReadChain.Build(s.handler(s.getBillingSettings)))
	router.HandleFunc(post(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint, common.PreviewEndpoint), privateWriteChain.Build(s.handler(s.postBillingPreview)))
	router.HandleFunc(put(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint), privateWriteChain.Build(s.handler(s.putBilling)))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint, common.CancelEndpoint), subscribedRead.Build(s.getCancelSubscription))
	router.HandleFunc(get(common.SettingsEndpoint, common.TabEndpoint, common.BillingEndpoint, common.UpdateEndpoint), subscribedRead.Build(s.getUpdateSubscription))
	router.HandleFunc(get(common.UserEndpoint, common.StatsEndpoint), privateReadChain.Build(common.NoCache(s.getAccountStats)))
	router.HandleFunc(delete(common.APIKeysEndpoint, arg(common.ParamKey)), privateWriteChain.Build(s.deleteAPIKey))
	router.HandleFunc(delete(common.UserEndpoint), privateWriteChain.Build(s.deleteAccount))
	router.HandleFunc(get(common.SupportEndpoint), privateReadChain.Build(s.handler(s.getSupport)))
	router.HandleFunc(post(common.SupportEndpoint), privateWriteChain.Build(s.handler(s.postSupport)))
	router.HandleFunc(http.MethodGet+" "+prefix+"{$}", privateReadChain.Build(s.getPortal))
	// wildcard
	router.HandleFunc(http.MethodGet+" "+prefix+"{path...}", corsHandler(ratelimiter(common.Logged(s.notFound))))
}

func (s *Server) handler(modelFunc ModelFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// such composition makes business logic and rendering testable separately
		renderCtx, tpl, err := modelFunc(w, r)
		if err != nil {
			switch err {
			case errInvalidSession:
				common.Redirect(s.relURL(common.LoginEndpoint), w, r)
			case errInvalidPathArg, errInvalidRequestArg:
				s.redirectError(http.StatusBadRequest, w, r)
			default:
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

func (s *Server) csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get(common.HeaderCSRFToken)
		if len(token) == 0 {
			token = r.FormValue(common.ParamCSRFToken)
		}

		if len(token) > 0 {
			sess := s.session(w, r)
			email, ok := sess.Get(session.KeyUserEmail).(string)
			if !ok {
				slog.WarnContext(ctx, "Session does not contain a valid email")
			}

			if s.XSRF.VerifyToken(token, email) {
				next.ServeHTTP(w, r)
				return
			} else {
				slog.WarnContext(ctx, "Failed to verify CSRF token")
			}
		} else {
			slog.WarnContext(ctx, "CSRF token is missing")
		}

		common.Redirect(s.relURL(common.ExpiredEndpoint), w, r)
	}
}
