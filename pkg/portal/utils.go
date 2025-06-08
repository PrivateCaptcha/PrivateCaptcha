package portal

import (
	"context"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

const (
	captchaSolutionField = "plSolution"
)

// NOTE: this will eventually be replaced by proper OTP
func twoFactorCode() int {
	return rand.Intn(900000) + 100000
}

// RouteGenerator's point is to passthrough the path correctly to the std.Handler() of slok/go-http-metrics
// the whole magic can break if for some reason Go will not evaluate result of Route() before calling Alice's Then()
// when calling router.Handle() in setupWithPrefix()
type RouteGenerator struct {
	Prefix string
	Path   string
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

func (s *Server) Org(userID int32, r *http.Request) (*dbgen.Organization, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return nil, errInvalidPathArg
	}

	org, err := s.Store.Impl().RetrieveUserOrganization(ctx, userID, int32(orgID))
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

	return org, nil
}

func (s *Server) OrgID(r *http.Request) (int32, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return -1, errInvalidPathArg
	}

	return int32(orgID), nil
}

func (s *Server) Property(orgID int32, r *http.Request) (*dbgen.Property, error) {
	ctx := r.Context()

	propertyID, value, err := common.IntPathArg(r, common.ParamProperty)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse property path parameter", "value", value, common.ErrAttr(err))
		return nil, errInvalidPathArg
	}

	property, err := s.Store.Impl().RetrieveOrgProperty(ctx, orgID, int32(propertyID))
	if err != nil {
		if err == db.ErrSoftDeleted {
			return nil, errPropertySoftDeleted
		}

		slog.ErrorContext(ctx, "Failed to find property by ID", common.ErrAttr(err))
		return nil, err
	}

	return property, nil
}

func (s *Server) Session(w http.ResponseWriter, r *http.Request) *common.Session {
	ctx := r.Context()
	sess, ok := ctx.Value(common.SessionContextKey).(*common.Session)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get session from context")
		sess = s.Sessions.SessionStart(w, r)
	}
	return sess
}

func (s *Server) SessionUser(ctx context.Context, sess *common.Session) (*dbgen.User, error) {
	userID, ok := sess.Get(session.KeyUserID).(int32)
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
	s.Sessions.SessionDestroy(w, r)
	common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusOK, w, r)
}

func (s *Server) CreateCaptchaRenderContext(sitekey string) CaptchaRenderContext {
	return CaptchaRenderContext{
		CaptchaEndpoint:      s.APIURL + "/" + common.PuzzleEndpoint,
		CaptchaDebug:         (s.Stage == common.StageDev) || (s.Stage == common.StageStaging),
		CaptchaSolutionField: captchaSolutionField,
		CaptchaSitekey:       sitekey,
	}
}

func (s *Server) createDemoCaptchaRenderContext(sitekey string) CaptchaRenderContext {
	return CaptchaRenderContext{
		CaptchaEndpoint:      "/" + common.EchoPuzzleEndpoint,
		CaptchaDebug:         (s.Stage == common.StageDev) || (s.Stage == common.StageStaging),
		CaptchaSolutionField: captchaSolutionField,
		CaptchaSitekey:       sitekey,
	}
}
