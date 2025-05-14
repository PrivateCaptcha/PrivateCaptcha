package portal

import (
	"context"
	"log/slog"
	"math/rand"
	"net/http"

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

func (s *Server) Org(userID int32, r *http.Request) (*dbgen.Organization, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return nil, errInvalidPathArg
	}

	org, err := s.Store.RetrieveUserOrganization(ctx, userID, int32(orgID))
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

	property, err := s.Store.RetrieveOrgProperty(ctx, orgID, int32(propertyID))
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

	user, err := s.Store.RetrieveUser(ctx, userID)
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

func (s *Server) static(tpl string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		renderCtx := &CsrfRenderContext{}
		s.render(w, r, tpl, renderCtx)
	})
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
