package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	renderContextNothing = struct{}{}
)

func (s *Server) postTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	sess := s.Sessions.SessionStart(w, r)
	ctx = context.WithValue(ctx, common.SessionIDContextKey, sess.ID())

	// "random" POST request to /twofactor with valid cookie might mean we access it from another node without this session
	// BUT if we have a local "weird" cached session, something is wrong and if it's not cached, it will be pulled from DB
	step, ok := sess.Get(ctx, session.KeyLoginStep).(int)
	if !ok || ((step != loginStepSignInVerify) && (step != loginStepSignUpVerify)) {
		slog.WarnContext(ctx, "User session is not valid", "step", step)
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	email, ok := sess.Get(ctx, session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	sentCode, ok := sess.Get(ctx, session.KeyTwoFactorCode).(int)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get verification code from session")
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	data := &loginRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(email),
		},
		Email: common.MaskEmail(email, '*'),
	}

	formCode := strings.TrimSpace(r.FormValue(common.ParamVerificationCode))
	if enteredCode, err := strconv.Atoi(formCode); (err != nil) || (enteredCode != sentCode) {
		data.CodeError = "Code is not valid."
		slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "expected", sentCode, common.ErrAttr(err))
		s.render(w, r, "login/twofactor-form.html", data)
		return
	}

	if step == loginStepSignUpVerify {
		slog.DebugContext(ctx, "Proceeding with the user registration flow after 2FA")
		if user, _, err := s.doRegister(ctx, sess); err == nil {
			_ = sess.Set(session.KeyUserID, user.ID)
			// NOTE: we can redirect user to create the first property instead of dashboard, but currently it's fine
			// redirectURL = s.partsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertyEndpoint, common.NewEndpoint)
		} else {
			slog.ErrorContext(ctx, "Failed to complete registration", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
	}

	userID, hasUserID := sess.Get(ctx, session.KeyUserID).(int32)
	if hasUserID {
		s.Store.AuditLog().RecordEvent(ctx, newUserAuthAuditLogEvent(userID, common.AuditLogActionLogin))

		go common.RunAdHocFunc(common.CopyTraceID(ctx, context.Background()), func(bctx context.Context) error {
			slog.DebugContext(bctx, "Fetching system notification for user", "userID", userID)
			if n, err := s.Store.Impl().RetrieveSystemUserNotification(bctx, time.Now().UTC(), userID); err == nil {
				_ = sess.Set(session.KeyNotificationID, n.ID)
			}

			return nil
		})
	} else {
		slog.ErrorContext(ctx, "UserID not found in session")
	}

	_ = sess.Set(session.KeyLoginStep, loginStepCompleted)
	_ = sess.Delete(session.KeyTwoFactorCode)
	_ = sess.Delete(session.KeyUserEmail)
	_ = sess.Set(session.KeyPersistent, true)

	if returnURL, ok := sess.Get(ctx, session.KeyReturnURL).(string); ok && (len(returnURL) > 0) {
		slog.DebugContext(ctx, "Found return URL in user session", "url", returnURL)
		_ = sess.Delete(session.KeyReturnURL)
		common.Redirect(s.RelURL(returnURL), http.StatusOK, w, r)
	} else {
		redirectURL := s.RelURL("/")
		common.Redirect(redirectURL, http.StatusOK, w, r)
	}
}

func (s *Server) resend2fa(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Sessions.SessionStart(w, r)
	if step, ok := sess.Get(ctx, session.KeyLoginStep).(int); !ok || ((step != loginStepSignInVerify) && (step != loginStepSignUpVerify)) {
		slog.WarnContext(ctx, "User session is not valid", "step", step)
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	email, ok := sess.Get(ctx, session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	code := twoFactorCode()
	location := r.Header.Get(s.CountryCodeHeader.Value())

	if err := s.Mailer.SendTwoFactor(ctx, email, code, r.UserAgent(), location); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.render(w, r, "login/resend-error.html", renderContextNothing)
		return
	}

	_ = sess.Set(session.KeyTwoFactorCode, code)
	s.render(w, r, "login/resend.html", renderContextNothing)
}
