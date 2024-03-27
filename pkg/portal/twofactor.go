package portal

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

const (
	twofactorFormTemplate = ""
)

type twoFactorRenderContext struct {
	Token string
	Email string
	Error string
}

func (s *Server) getTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); !ok || ((step != loginStepSignInVerify) && (step != loginStepSignUpVerify)) {
		slog.WarnContext(ctx, "User session is not valid", "step", step)
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	data := &twoFactorRenderContext{
		Token: s.XSRF.Token(email, actionVerify),
		Email: common.MaskEmail(email, '*'),
	}

	s.render(ctx, w, "twofactor/twofactor.html", data)
}

func (s *Server) postTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	step, ok := sess.Get(session.KeyLoginStep).(int)
	if !ok || ((step != loginStepSignInVerify) && (step != loginStepSignUpVerify)) {
		slog.WarnContext(ctx, "User session is not valid")
		s.htmxRedirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		s.htmxRedirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	data := &twoFactorRenderContext{
		Token: s.XSRF.Token(email, actionVerify),
		Email: common.MaskEmail(email, '*'),
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormSizeBytes)

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusBadRequest, w, r)
		return
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, email, actionVerify) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		s.htmxRedirect(s.relURL(common.ExpiredEndpoint), w, r)
		return
	}

	sentCode, ok := sess.Get(session.KeyTwoFactorCode).(int)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get verification code from session")
		s.htmxRedirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	formCode := r.FormValue(common.ParamVerificationCode)
	if enteredCode, err := strconv.Atoi(formCode); (err != nil) || (enteredCode != sentCode) {
		data.Error = "Code is not valid."
		slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "expected", sentCode, common.ErrAttr(err))
		s.render(ctx, w, "twofactor/form.html", data)
		return
	}

	if step == loginStepSignUpVerify {
		slog.DebugContext(ctx, "Proceeding with the user registration flow after 2FA")
		if err = s.doRegister(ctx, sess); err != nil {
			slog.ErrorContext(ctx, "Failed to complete registration", common.ErrAttr(err))
			s.htmxRedirectError(http.StatusInternalServerError, w, r)
			return
		}
	}

	sess.Set(session.KeyLoginStep, loginStepCompleted)
	sess.Set(session.KeyPersistent, true)
	// TODO: Redirect user to create the first property instead of dashboard
	// in case we're registering
	s.htmxRedirect(s.relURL("/"), w, r)
}

func (s *Server) resend2fa(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); !ok || ((step != loginStepSignInVerify) && (step != loginStepSignUpVerify)) {
		slog.WarnContext(ctx, "User session is not valid", "step", step)
		s.htmxRedirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		s.htmxRedirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.render(ctx, w, "twofactor/resend-error.html", struct{}{})
		return
	}

	sess.Set(session.KeyTwoFactorCode, code)
	s.render(ctx, w, "twofactor/resend.html", struct{}{})
}
