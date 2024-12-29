package portal

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
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

func (s *Server) org(r *http.Request) (*dbgen.Organization, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return nil, errInvalidPathArg
	}

	org, err := s.Store.RetrieveOrganization(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find org by ID", common.ErrAttr(err))
		return nil, err
	}

	return org, nil
}

func (s *Server) orgID(r *http.Request) (int32, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return -1, errInvalidPathArg
	}

	return int32(orgID), nil
}

func (s *Server) property(r *http.Request) (*dbgen.Property, error) {
	ctx := r.Context()

	propertyID, value, err := common.IntPathArg(r, common.ParamProperty)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse property path parameter", "value", value, common.ErrAttr(err))
		return nil, errInvalidPathArg
	}

	property, err := s.Store.RetrieveProperty(ctx, int32(propertyID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find property by ID", common.ErrAttr(err))
		return nil, err
	}

	return property, nil
}

func (s *Server) propertyID(r *http.Request) (int32, error) {
	ctx := r.Context()

	propertyID, value, err := common.IntPathArg(r, common.ParamProperty)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse property path parameter", "value", value, common.ErrAttr(err))
		return 0, errInvalidPathArg
	}

	return int32(propertyID), nil
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

func (s *Server) sessionUser(ctx context.Context, sess *common.Session) (*dbgen.User, error) {
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

func (s *Server) subscribed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		user, err := s.sessionUser(ctx, s.session(w, r))
		if err != nil {
			common.Redirect(s.relURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
			return
		}

		billingPath := fmt.Sprintf("%s?%s=%s", common.SettingsEndpoint, common.ParamTab, common.BillingEndpoint)

		if !user.SubscriptionID.Valid {
			slog.WarnContext(ctx, "User does not have a subscription", "userID", user.ID)
			url := s.relURL(billingPath)
			common.Redirect(url, http.StatusPaymentRequired, w, r)
			return
		}

		if subscr, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32); err == nil {
			if !billing.IsSubscriptionActive(subscr.Status) {
				slog.WarnContext(ctx, "User's subscription is not active", "status", subscr.Status, "userID", user.ID)
				url := s.relURL(billingPath)
				common.Redirect(url, http.StatusPaymentRequired, w, r)
				return
			}
		} else {
			slog.ErrorContext(ctx, "Failed to check user subscription", "userID", user.ID, common.ErrAttr(err))
			s.redirectError(http.StatusInternalServerError, w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Session.SessionDestroy(w, r)
	common.Redirect(s.relURL(common.LoginEndpoint), http.StatusOK, w, r)
}

func (s *Server) static(tpl string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		renderCtx := &csrfRenderContext{}
		s.render(w, r, tpl, renderCtx)
	})
}

func (s *Server) createCaptchaRenderContext() captchaRenderContext {
	return captchaRenderContext{
		CaptchaEndpoint:      s.APIURL + "/" + common.PuzzleEndpoint,
		CaptchaDebug:         s.Stage != common.StageProd,
		CaptchaSolutionField: captchaSolutionField,
	}
}
