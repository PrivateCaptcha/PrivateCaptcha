package portal

import (
	"context"
	"crypto/rand"
	"log/slog"
	"math/big"
	randv2 "math/rand/v2"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

// NOTE: this will eventually be replaced by proper OTP
func twoFactorCode(ctx context.Context) int {
	const span = 900000
	const start = 100000
	n, err := rand.Int(rand.Reader, big.NewInt(span))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate two-factor code using crypto/rand", common.ErrAttr(err))

		return randv2.IntN(span) + start
	}

	return int(n.Int64()) + start
}

func (s *Server) Org(user *dbgen.User, r *http.Request) (*dbgen.Organization, dbgen.NullAccessLevel, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return nil, dbgen.NullAccessLevel{}, ErrInvalidPathArg
	}

	org, level, err := s.Store.Impl().RetrieveUserOrganization(ctx, user, orgID)
	if err != nil {
		if err == db.ErrSoftDeleted {
			return nil, dbgen.NullAccessLevel{}, errOrgSoftDeleted
		}

		if err == db.ErrPermissions {
			return nil, dbgen.NullAccessLevel{}, db.ErrPermissions
		}

		slog.ErrorContext(ctx, "Failed to find org by ID", common.ErrAttr(err))
		return nil, dbgen.NullAccessLevel{}, err
	}

	if !s.checkUserOrgAccess(user, org) {
		slog.ErrorContext(ctx, "User cannot use this org", "userID", user.ID, "orgID", orgID, "enterprise", s.isEnterprise())
		return nil, dbgen.NullAccessLevel{}, errLimitedFeature
	}

	return org, level, nil
}

func (s *Server) OrgID(r *http.Request) (int32, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return -1, ErrInvalidPathArg
	}

	return int32(orgID), nil
}

func (s *Server) Property(org *dbgen.Organization, r *http.Request) (*dbgen.Property, error) {
	ctx := r.Context()

	propertyID, value, err := common.IntPathArg(r, common.ParamProperty, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse property path parameter", "value", value, common.ErrAttr(err))
		return nil, ErrInvalidPathArg
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

func (s *Server) Form(org *dbgen.Organization, r *http.Request) (*dbgen.Form, error) {
	ctx := r.Context()

	formID, value, err := common.IntPathArg(r, common.ParamForm, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form path parameter", "value", value, common.ErrAttr(err))
		return nil, ErrInvalidPathArg
	}

	form, err := s.Store.Impl().RetrieveOrgForm(ctx, org, formID)
	if err != nil {
		if err == db.ErrSoftDeleted {
			return nil, errFormSoftDeleted
		}

		slog.ErrorContext(ctx, "Failed to find form by ID", common.ErrAttr(err))
		return nil, err
	}

	return form, nil
}

func (s *Server) Session(_ http.ResponseWriter, r *http.Request) *session.Session {
	ctx := r.Context()
	sess, _ := ctx.Value(common.SessionContextKey).(*session.Session)
	if sess == nil {
		var err error
		sess, err = s.Sessions.Get(r)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to get authenticated session", common.ErrAttr(err))
		}
	}
	return sess
}

func (s *Server) SessionUser(ctx context.Context, sess *session.Session) (*dbgen.User, error) {
	if sess == nil {
		return nil, ErrInvalidSession
	}
	authority, ok := sess.Authority()
	if !ok || authority.State != session.StateAuthenticated || authority.UserID <= 0 {
		slog.ErrorContext(ctx, "Failed to get userID from session")
		return nil, ErrInvalidSession
	}

	user, err := s.Store.Impl().RetrieveUser(ctx, authority.UserID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by ID", "id", authority.UserID, common.ErrAttr(err))
		return nil, err
	}

	return user, nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	result, err := s.Sessions.Revoke(w, r)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to revoke session during logout", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}
	if result != nil && result.UserID > 0 {
		ctx = context.WithValue(ctx, common.SessionHashContextKey, result.SessionHash)
		s.Store.AuditLog().RecordEvent(ctx, newUserAuthAuditLogEvent(result.UserID, common.AuditLogActionLogout), common.AuditLogSourcePortal)
		s.Store.Impl().CleanupUserCache(ctx, result.UserID)
	}
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
		CaptchaEndpoint:      s.RelURL(common.EchoPuzzleEndpoint),
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
