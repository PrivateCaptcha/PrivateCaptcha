package portal

import (
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/badoux/checkmail"
)

const (
	maxLoginFormSizeBytes = 10 * 1024
	loginStepSignInVerify = 1
	loginStepSignUpVerify = 2
	loginStepCompleted    = 3
	loginFormTemplate     = "login/form.html"
)

type loginRenderContext struct {
	Token string
	Error string
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	data := &loginRenderContext{
		Token: s.XSRF.Token("", actionLogin),
	}
	s.render(r.Context(), w, r, "login/login.html", data)
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormSizeBytes)
	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusBadRequest, w, r)
		return
	}

	data := &loginRenderContext{
		Token: s.XSRF.Token("", actionLogin),
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, "", actionLogin) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		data.Error = "Please try again."
		s.render(ctx, w, r, loginFormTemplate, data)
		return
	}

	email := r.FormValue(common.ParamEmail)
	if err = checkmail.ValidateFormat(email); err != nil {
		slog.Warn("Failed to validate email format", common.ErrAttr(err))
		data.Error = "Email address is not valid."
		s.render(r.Context(), w, r, loginFormTemplate, data)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.WarnContext(ctx, "Failed to find user by email", "email", email)
		data.Error = "User with such email does not exist."
		s.render(ctx, w, r, loginFormTemplate, data)
		return
	}

	sess := s.Session.SessionStart(w, r)
	if step, ok := sess.Get(session.KeyLoginStep).(int); ok {
		if step == loginStepCompleted {
			slog.DebugContext(ctx, "User seem to be already logged in", "email", email)
			s.htmxRedirect(s.relURL("/"), w, r)
			return
		} else {
			slog.WarnContext(ctx, "Session present, but login not finished", "step", step, "email", email)
		}
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	sess.Set(session.KeyLoginStep, loginStepSignInVerify)
	sess.Set(session.KeyUserEmail, user.Email)
	sess.Set(session.KeyTwoFactorCode, code)

	s.htmxRedirect(s.relURL(common.TwoFactorEndpoint), w, r)
}
