package portal

import (
	"context"
	"log/slog"
	randv2 "math/rand/v2"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/justinas/alice"
)

// NOTE: this will eventually be replaced by proper OTP
func twoFactorCode() int {
	return randv2.IntN(900000) + 100000
}

type RouteAndHandler struct {
	pattern string
	chain   alice.Chain
	handler http.Handler
}

// RouteGenerator's point is to passthrough the path correctly to the std.Handler() of slok/go-http-metrics
// the whole magic can break if for some reason Go will not evaluate result of Route() before calling Alice's Then()
// when calling router.Handle() in setupWithPrefix()
type RouteGenerator struct {
	Prefix string
	Path   string
	routes []*RouteAndHandler
}

func (rg *RouteGenerator) Route(method string, parts ...string) string {
	rg.Path = strings.Join(parts, "/")
	result := method + " " + rg.Prefix + rg.Path
	return result
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

func (rg *RouteGenerator) Handler(pattern string) (*RouteAndHandler, bool) {
	for _, route := range rg.routes {
		if route.pattern == pattern {
			return route, true
		}
	}

	return nil, false
}

func (rg *RouteGenerator) Handle(pattern string, chain alice.Chain, handler http.Handler) {
	if route, ok := rg.Handler(pattern); ok {
		route.chain = chain
		route.handler = handler
		return
	}

	rg.routes = append(rg.routes, &RouteAndHandler{
		pattern: pattern,
		chain:   chain,
		handler: handler,
	})
}

func (rg *RouteGenerator) Register(router *http.ServeMux) {
	for _, route := range rg.routes {
		router.Handle(route.pattern, route.chain.Then(route.handler))
	}
}

func (s *Server) Org(user *dbgen.User, r *http.Request) (*dbgen.Organization, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return nil, errInvalidPathArg
	}

	org, err := s.Store.Impl().RetrieveUserOrganization(ctx, user, orgID)
	if err != nil {
		if err == db.ErrSoftDeleted {
			return nil, errOrgSoftDeleted
		}

		if err == db.ErrPermissions {
			return nil, db.ErrPermissions
		}

		slog.ErrorContext(ctx, "Failed to find org by ID", common.ErrAttr(err))
		return nil, err
	}

	if !s.checkUserOrgAccess(user, org) {
		slog.ErrorContext(ctx, "User cannot use this org", "userID", user.ID, "orgID", orgID, "enterprise", s.isEnterprise())
		return nil, errLimitedFeature
	}

	return org, nil
}

func (s *Server) OrgID(r *http.Request) (int32, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return -1, errInvalidPathArg
	}

	return int32(orgID), nil
}

func (s *Server) Property(org *dbgen.Organization, r *http.Request) (*dbgen.Property, error) {
	ctx := r.Context()

	propertyID, value, err := common.IntPathArg(r, common.ParamProperty, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse property path parameter", "value", value, common.ErrAttr(err))
		return nil, errInvalidPathArg
	}

	property, err := s.Store.Impl().RetrieveOrgProperty(ctx, org, propertyID)
	if err != nil {
		if err == db.ErrSoftDeleted {
			return nil, errPropertySoftDeleted
		}

		slog.ErrorContext(ctx, "Failed to find property by ID", common.ErrAttr(err))
		return nil, err
	}

	return property, nil
}

func (s *Server) Session(w http.ResponseWriter, r *http.Request) *session.Session {
	ctx := r.Context()
	sess, ok := ctx.Value(common.SessionContextKey).(*session.Session)
	if !ok || (sess == nil) {
		slog.ErrorContext(ctx, "Failed to get session from context")
		var found bool
		sess, found = s.Sessions.SessionGet(r)
		if !found || (sess == nil) {
			slog.ErrorContext(ctx, "Failed to get started session")
			sess = s.Sessions.SessionStart(w, r)
		}
	}

	return sess
}

func (s *Server) SessionUser(ctx context.Context, sess *session.Session) (*dbgen.User, error) {
	userID, ok := sess.Get(ctx, session.KeyUserID).(int32)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get userID from session")
		return nil, errInvalidSession
	}

	user, err := s.Store.Impl().RetrieveUser(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by ID", "id", userID, common.ErrAttr(err))
		return nil, err
	}

	return user, nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session(w, r)
	if userID, ok := sess.Get(ctx, session.KeyUserID).(int32); ok {
		s.Store.AuditLog().RecordEvent(ctx, newUserAuthAuditLogEvent(userID, common.AuditLogActionLogout))
	}

	s.Sessions.SessionDestroy(w, r)
	common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusOK, w, r)
}

func (s *Server) CreateCaptchaRenderContext(sitekey string) CaptchaRenderContext {
	return CaptchaRenderContext{
		CaptchaEndpoint:      s.APIURL + "/" + common.PuzzleEndpoint,
		CaptchaDebug:         (s.Stage == common.StageDev) || (s.Stage == common.StageStaging),
		CaptchaSolutionField: common.ParamPortalSolution,
		CaptchaSitekey:       sitekey,
	}
}

func (s *Server) createDemoCaptchaRenderContext(sitekey string) CaptchaRenderContext {
	return CaptchaRenderContext{
		CaptchaEndpoint:      "/" + common.EchoPuzzleEndpoint,
		CaptchaDebug:         (s.Stage == common.StageDev) || (s.Stage == common.StageStaging),
		CaptchaSolutionField: common.ParamPortalSolution,
		CaptchaSitekey:       sitekey,
	}
}

func newAccessAuditLogEvent(user *dbgen.User, tableName string, entityID int64, entityName string, view string) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionAccess,
		EntityID:  entityID,
		TableName: tableName,
		NewValue:  &db.AuditLogAccess{View: view, EntityName: entityName},
	}
}

func newUserAuthAuditLogEvent(userID int32, action common.AuditLogAction) *common.AuditLogEvent {
	return &common.AuditLogEvent{
		UserID:    userID,
		Action:    action,
		EntityID:  int64(userID),
		TableName: db.TableNameUsers,
		OldValue:  nil,
		NewValue:  nil,
	}
}
