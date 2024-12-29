package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/badoux/checkmail"
)

var (
	errIncompleteSession    = errors.New("data in session is incomplete")
	errRegistrationDisabled = errors.New("registration disabled")
)

const (
	registerFormTemplate = "register/form.html"
)

type registerRenderContext struct {
	csrfRenderContext
	captchaRenderContext
	NameError       string
	EmailError      string
	RegisterSitekey string
}

func (s *Server) getRegister(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	if !s.canRegister.Load() {
		return nil, "", errRegistrationDisabled
	}

	return &registerRenderContext{
		csrfRenderContext:    csrfRenderContext{},
		captchaRenderContext: s.createCaptchaRenderContext(),
		RegisterSitekey:      strings.ReplaceAll(db.PortalRegisterPropertyID, "-", ""),
	}, "register/register.html", nil
}

func (s *Server) postRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	if !s.canRegister.Load() {
		slog.WarnContext(ctx, "Registration is disabled")
		s.redirectError(http.StatusNotImplemented, w, r)
		return
	}

	data := &registerRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		captchaRenderContext: s.createCaptchaRenderContext(),
		RegisterSitekey:      strings.ReplaceAll(db.PortalRegisterPropertyID, "-", ""),
	}

	ownerSource := &portalPropertyOwnerSource{Store: s.Store, Sitekey: data.RegisterSitekey}

	captchaSolution := r.FormValue(captchaSolutionField)
	verr, err := s.Verifier.Verify(ctx, captchaSolution, ownerSource, time.Now().UTC())
	if err != nil || verr != puzzle.VerifyNoError {
		slog.ErrorContext(ctx, "Failed to verify captcha", "code", verr, common.ErrAttr(err))
		data.CaptchaError = "Captcha verification failed."
		s.render(w, r, registerFormTemplate, data)
		return
	}

	name := strings.TrimSpace(r.FormValue(common.ParamName))
	if len(name) < 3 {
		data.NameError = "Please use a longer name."
		s.render(w, r, registerFormTemplate, data)
		return
	}

	email := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if err := checkmail.ValidateFormat(email); err != nil {
		slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
		data.EmailError = "Email address is not valid."
		s.render(w, r, registerFormTemplate, data)
		return
	}

	if _, err := s.Store.FindUserByEmail(ctx, email); err == nil {
		slog.WarnContext(ctx, "User with such email already exists", "email", email)
		data.EmailError = "Such email is already registered. Login instead?"
		s.render(w, r, registerFormTemplate, data)
		return
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	sess := s.Session.SessionStart(w, r)
	sess.Set(session.KeyLoginStep, loginStepSignUpVerify)
	sess.Set(session.KeyUserEmail, email)
	sess.Set(session.KeyUserName, name)
	sess.Set(session.KeyTwoFactorCode, code)

	common.Redirect(s.relURL(common.TwoFactorEndpoint), http.StatusOK, w, r)
}

func (s *Server) doRegister(ctx context.Context, sess *common.Session) (*dbgen.User, error) {
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		return nil, errIncompleteSession
	}

	name, ok := sess.Get(session.KeyUserName).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get user name from session")
		return nil, errIncompleteSession
	}

	orgName := common.OrgNameFromName(name)

	user, _, err := s.Store.CreateNewAccount(ctx, nil /*subscription*/, email, name, orgName, -1 /*existing user ID*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create user account in Store", common.ErrAttr(err))
		return nil, err
	}

	if plans, ok := billing.GetPlansForStage(s.Stage); ok && len(plans) > 0 {
		// seed no-subscription user with the smallest plan's limits
		plan := plans[0]
		_ = s.TimeSeries.UpdateUserLimits(ctx, map[int32]int64{user.ID: plan.RequestsLimit})
	}

	return user, nil
}
