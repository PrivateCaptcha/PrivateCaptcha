package portal

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

const (
	maxLoginFormSizeBytes = 10 * 1024
	loginStepTwoFactor    = 1
	loginStepCompleted    = 2
)

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Token string
	}{
		Token: s.XSRF.Token("", actionLogin),
	}
	s.render(r.Context(), w, "login/login.html", data)
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := struct {
		Token string
		Error string
	}{
		Token: s.XSRF.Token("", actionLogin),
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormSizeBytes)
	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusBadRequest, w, r)
		return
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, "", actionLogin) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		data.Error = "Please try again."
		s.render(ctx, w, "login/email-error.html", data)
		return
	}

	email := r.FormValue(common.ParamEmail)
	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.WarnContext(ctx, "Failed to find user by email", "email", email)
		data.Error = "User with such email does not exist."
		s.render(ctx, w, "login/email-error.html", data)
		return
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	sess := s.Session.SessionStart(w, r)
	sess.Set(session.KeyLoginStep, loginStepTwoFactor)
	sess.Set(session.KeyUserEmail, user.Email)
	sess.Set(session.KeyTwoFactorCode, code)

	s.htmxRedirect(s.relURL(common.TwoFactorEndpoint), w, r)
}

func (s *Server) getTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); !ok || step != loginStepTwoFactor {
		slog.WarnContext(ctx, "User session is not valid")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	data := struct {
		Token string
		Email string
		Error string
	}{
		Token: s.XSRF.Token(email, actionVerify),
		Email: common.MaskEmail(email, '*'),
	}

	s.render(ctx, w, "login/twofactor.html", data)
}

func (s *Server) postTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); !ok || step != loginStepTwoFactor {
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

	data := struct {
		Token string
		Email string
		Error string
	}{
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
		s.htmxRedirectError(http.StatusUnauthorized, w, r)
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
		s.render(ctx, w, "login/code-error.html", data)
		return
	}

	sess.Set(session.KeyLoginStep, loginStepCompleted)
	s.htmxRedirect(s.relURL("/"), w, r)
}

func (s *Server) resend2fa(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); !ok || step != loginStepTwoFactor {
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
		s.render(ctx, w, "login/resend-error.html", struct{}{})
		return
	}

	sess.Set(session.KeyTwoFactorCode, code)
	s.render(ctx, w, "login/resend.html", struct{}{})
}
