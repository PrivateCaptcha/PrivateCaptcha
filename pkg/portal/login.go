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
	loginStepSignInVerify     = 1
	loginStepSignUpVerify     = 2
	loginStepCompleted        = 3
	loginTemplate             = "login/login.html"
	loginContentsTemplate     = "login/login-contents.html"
	captchaVerificationFailed = "Captcha verification failed."
	twofactorContentsTemplate = "login/twofactor-contents.html"
)

var (
	errPortalPropertyNotFound = errors.New("portal property not found")
)

type loginRenderContext struct {
	CsrfRenderContext
	CaptchaRenderContext
	Email       string
	EmailError  string
	CodeError   string
	NameError   string
	CanRegister bool
	IsRegister  bool
}

type portalPropertyOwnerSource struct {
	Store   db.Implementor
	Sitekey string
}

var _ puzzle.OwnerIDSource = (*portalPropertyOwnerSource)(nil)

func (s *portalPropertyOwnerSource) OwnerID(ctx context.Context, tnow time.Time) (int32, *int32, error) {
	property, err := s.Store.Impl().RetrievePropertyBySitekey(ctx, s.Sitekey)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch login property", common.ErrAttr(err))
		return -1, nil, errPortalPropertyNotFound
	}

	orgID := new(int32)
	*orgID = property.OrgID.Int32

	return property.OrgOwnerID.Int32, orgID, nil
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	return &ViewModel{
		Model: &loginRenderContext{
			CsrfRenderContext: CsrfRenderContext{
				Token: s.XSRF.Token(""),
			},
			CaptchaRenderContext: s.CreateCaptchaRenderContext(db.PortalLoginSitekey),
			CanRegister:          s.canRegister.Load(),
		},
		View: loginTemplate,
	}, nil
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	data := &loginRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		CaptchaRenderContext: s.CreateCaptchaRenderContext(db.PortalLoginSitekey),
		CanRegister:          s.canRegister.Load(),
	}

	captchaSolution := r.FormValue(common.ParamPortalSolution)
	if len(captchaSolution) == 0 {
		slog.WarnContext(ctx, "Captcha solution field is empty")
		data.CaptchaError = "You need to solve captcha to login."
		s.render(w, r, loginContentsTemplate, data)
		return
	}

	payload, err := s.PuzzleEngine.ParseSolutionPayload(ctx, []byte(captchaSolution))
	if err != nil {
		data.CaptchaError = captchaVerificationFailed
		s.render(w, r, loginContentsTemplate, data)
		return
	}

	ownerSource := &portalPropertyOwnerSource{Store: s.Store, Sitekey: data.CaptchaSitekey}
	verifyResult, err := s.PuzzleEngine.Verify(ctx, payload, ownerSource, time.Now().UTC())
	if err != nil {
		slog.ErrorContext(ctx, "Failed to verify captcha due to internal error", common.ErrAttr(err))
		data.CaptchaError = captchaVerificationFailed
		s.render(w, r, loginContentsTemplate, data)
		return
	}
	if !verifyResult.Success() {
		slog.ErrorContext(ctx, "Failed to verify captcha", "errors", verifyResult.Error.String())
		data.CaptchaError = captchaVerificationFailed
		s.render(w, r, loginContentsTemplate, data)
		return
	}

	email := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if err = checkmail.ValidateFormat(email); err != nil {
		slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
		data.EmailError = "Email address is not valid."
		s.render(w, r, loginContentsTemplate, data)
		return
	}

	user, err := s.Store.Impl().FindUserByEmail(ctx, email)
	if err != nil {
		if err == db.ErrDisabled {
			slog.WarnContext(ctx, "Disabled user attempted to login", "email", email)
			data.EmailError = "This account has been disabled."
			s.render(w, r, loginContentsTemplate, data)
			return
		}
		slog.WarnContext(ctx, "Failed to find active user by email", "email", email, common.ErrAttr(err))
		data.EmailError = "User with such email does not exist."
		s.render(w, r, loginContentsTemplate, data)
		return
	}

	sess := s.Sessions.SessionStart(w, r)
	if step, ok := sess.Get(ctx, session.KeyLoginStep).(int); ok {
		if step == loginStepCompleted {
			slog.DebugContext(ctx, "User seem to be already logged in", "email", email)
			common.Redirect(s.RelURL("/"), http.StatusOK, w, r)
			return
		} else {
			slog.WarnContext(ctx, "Session present, but login not finished", "step", step, "email", email)
		}
	}

	code := twoFactorCode(ctx)
	location := r.Header.Get(s.CountryCodeHeader.Value())

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code, r.UserAgent(), location, false); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	_ = sess.Set(ctx, session.KeyLoginStep, loginStepSignInVerify)
	_ = sess.Set(ctx, session.KeyUserEmail, user.Email)
	_ = sess.Set(ctx, session.KeyUserName, user.Name)
	_ = sess.Set(ctx, session.KeyTwoFactorCode, code)
	_ = sess.Set(ctx, session.KeyTwoFactorCodeTimestamp, time.Now().UTC())
	_ = sess.Set(ctx, session.KeyUserID, user.ID)
	// we clean this flag because at this point the user should either be registered or subscribed
	_ = sess.Delete(ctx, session.KeyVerifyRegistration)
	// this is needed in case we will be routed to another server that does not have our session in memory
	// (previously we persisted ONLY logged in sessions, but if we're rerouted during login, it will break)
	// this should be OK now because we verified that user is a registered user AND they solved captcha
	_ = sess.Set(ctx, session.KeyPersistent, true)

	data.Token = s.XSRF.Token(user.Email)
	data.Email = common.MaskEmail(user.Email, '*')

	s.render(w, r, twofactorContentsTemplate, data)
}
