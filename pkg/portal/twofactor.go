package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

const (
	twofactorFormTemplate = ""
	twofactorTemplate     = "twofactor/twofactor.html"
)

type twoFactorRenderContext struct {
	csrfRenderContext
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
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(email),
		},
		Email: common.MaskEmail(email, '*'),
	}

	s.render(w, r, twofactorTemplate, data)
}

func (s *Server) postTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	step, ok := sess.Get(session.KeyLoginStep).(int)
	if !ok || ((step != loginStepSignInVerify) && (step != loginStepSignUpVerify)) {
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
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(email),
		},
		Email: common.MaskEmail(email, '*'),
	}

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	sentCode, ok := sess.Get(session.KeyTwoFactorCode).(int)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get verification code from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	formCode := r.FormValue(common.ParamVerificationCode)
	if enteredCode, err := strconv.Atoi(formCode); (err != nil) || (enteredCode != sentCode) {
		data.Error = "Code is not valid."
		slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "expected", sentCode, common.ErrAttr(err))
		s.render(w, r, "twofactor/form.html", data)
		return
	}

	if step == loginStepSignUpVerify {
		slog.DebugContext(ctx, "Proceeding with the user registration flow after 2FA")
		if err = s.doRegister(ctx, sess); err != nil {
			slog.ErrorContext(ctx, "Failed to complete registration", common.ErrAttr(err))
			s.redirectError(http.StatusInternalServerError, w, r)
			return
		}
	}

	go func(bctx context.Context) {
		if userID, ok := sess.Get(session.KeyUserID).(int32); ok {
			slog.DebugContext(bctx, "Fetching system notification for user", "userID", userID)
			if n, err := s.Store.RetrieveUserNotification(bctx, time.Now().UTC(), userID); err == nil {
				sess.Set(session.KeyNotificationID, n.ID)
			}
		} else {
			slog.WarnContext(bctx, "UserID not found in session")
		}
	}(common.CopyTraceID(ctx, context.Background()))

	sess.Set(session.KeyLoginStep, loginStepCompleted)
	sess.Delete(session.KeyTwoFactorCode)
	sess.Set(session.KeyPersistent, true)
	// TODO: Redirect user to create the first property instead of dashboard
	// in case we're registering
	common.Redirect(s.relURL("/"), w, r)
}

func (s *Server) resend2fa(w http.ResponseWriter, r *http.Request) {
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

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.render(w, r, "twofactor/resend-error.html", struct{}{})
		return
	}

	sess.Set(session.KeyTwoFactorCode, code)
	s.render(w, r, "twofactor/resend.html", struct{}{})
}
