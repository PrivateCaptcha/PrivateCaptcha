package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/badoux/checkmail"
)

const (
	loginStepSignInVerify = 1
	loginStepSignUpVerify = 2
	loginStepCompleted    = 3
	loginFormTemplate     = "login/form.html"
	loginTemplate         = "login/login.html"
	loginSolutionField    = "plSolution"
)

var (
	errPortalPropertyNotFound = errors.New("portal property not found")
)

type loginRenderContext struct {
	csrfRenderContext
	LoginSitekey         string
	EmailError           string
	CaptchaError         string
	CaptchaEndpoint      string
	CaptchaSolutionField string
	CaptchaDebug         bool
}

type portalPropertyOwnerSource struct {
	Store   *db.BusinessStore
	Sitekey string
}

func (s *portalPropertyOwnerSource) OwnerID(ctx context.Context) (int32, error) {
	properties, err := s.Store.RetrievePropertiesBySitekey(ctx, map[string]struct{}{s.Sitekey: {}})
	if (err != nil) || (len(properties) != 1) {
		slog.ErrorContext(ctx, "Failed to fetch login property", common.ErrAttr(err))
		return -1, errPortalPropertyNotFound
	}

	return properties[0].OrgOwnerID.Int32, nil
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	return &loginRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		LoginSitekey:         strings.ReplaceAll(db.PortalPropertyID, "-", ""),
		CaptchaEndpoint:      s.APIURL + "/" + common.PuzzleEndpoint,
		CaptchaDebug:         s.Stage != common.StageProd,
		CaptchaSolutionField: loginSolutionField,
	}, loginTemplate, nil
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	data := &loginRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		LoginSitekey:         strings.ReplaceAll(db.PortalPropertyID, "-", ""),
		CaptchaEndpoint:      s.APIURL + "/" + common.PuzzleEndpoint,
		CaptchaDebug:         s.Stage != common.StageProd,
		CaptchaSolutionField: loginSolutionField,
	}

	ownerSource := &portalPropertyOwnerSource{Store: s.Store, Sitekey: data.LoginSitekey}

	captchaSolution := r.FormValue(loginSolutionField)
	verr, err := s.Verifier.Verify(ctx, captchaSolution, ownerSource, time.Now().UTC())
	if err != nil || verr != puzzle.VerifyNoError {
		data.CaptchaError = "Captcha verification failed"
		s.render(w, r, loginFormTemplate, data)
		return
	}

	email := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if err = checkmail.ValidateFormat(email); err != nil {
		slog.Warn("Failed to validate email format", common.ErrAttr(err))
		data.EmailError = "Email address is not valid."
		s.render(w, r, loginFormTemplate, data)
		return
	}

	user, err := s.Store.FindUserByEmail(ctx, email)
	if err != nil {
		slog.WarnContext(ctx, "Failed to find user by email", "email", email)
		data.EmailError = "User with such email does not exist."
		s.render(w, r, loginFormTemplate, data)
		return
	}

	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); ok {
		if step == loginStepCompleted {
			slog.DebugContext(ctx, "User seem to be already logged in", "email", email)
			common.Redirect(s.relURL("/"), w, r)
			return
		} else {
			slog.WarnContext(ctx, "Session present, but login not finished", "step", step, "email", email)
		}
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	sess.Set(session.KeyLoginStep, loginStepSignInVerify)
	sess.Set(session.KeyUserEmail, user.Email)
	sess.Set(session.KeyUserName, user.Name)
	sess.Set(session.KeyTwoFactorCode, code)
	sess.Set(session.KeyUserID, user.ID)

	common.Redirect(s.relURL(common.TwoFactorEndpoint), w, r)
}
