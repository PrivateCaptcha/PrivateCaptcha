package portal

import (
	"context"
	"fmt"
	"log/slog"
	"math"
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
	tnow := time.Now().UTC()
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

	// During HTMX flow, the browser URL stays at the org invite URL even when we POST to 2FA
	// If org invite ID is not in session, check the Referer header to extract it
	if _, hasOrgInvite := sess.Get(ctx, session.KeyOrgInviteID).(int32); !hasOrgInvite {
		if referer := r.Header.Get(common.HeaderReferer); len(referer) > 0 {
			if inviteID := s.parseOrgInviteIDFromURL(referer); inviteID > 0 {
				slog.DebugContext(ctx, "Parsed org invite ID from Referer header", "inviteID", inviteID)
				_ = sess.Set(ctx, session.KeyOrgInviteID, inviteID)
			}
		}
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

	codeTimestamp, ok := sess.Get(ctx, session.KeyTwoFactorCodeTimestamp).(time.Time)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get verification code timestamp")
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
	if enteredCode, err := strconv.Atoi(formCode); (err != nil) || (enteredCode != sentCode) || (!codeTimestamp.IsZero() && tnow.After(codeTimestamp.Add(s.TwoFactorDuration))) {
		data.CodeError = "Code is not valid."
		slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "timestamp", codeTimestamp, common.ErrAttr(err))
		s.render(w, r, "login/twofactor-form.html", data)
		return
	}

	var newRegistrationRedirectURL string
	rootRedirectURL := s.RelURL("/")

	if step == loginStepSignUpVerify {
		if _, ok := sess.Get(ctx, session.KeyVerifyRegistration).(bool); ok {
			slog.WarnContext(ctx, "Account requires an additional verification", "email", email)
			common.Redirect(s.RelURL(common.AccountVerifyEndpoint), http.StatusUnauthorized, w, r)
			return
		}

		slog.DebugContext(ctx, "Proceeding with the user registration flow after 2FA")
		if user, _, err := s.doRegister(ctx, sess); err == nil {
			_ = sess.Set(ctx, session.KeyUserID, user.ID)
			_ = sess.Set(ctx, session.KeyFirstSession, true)
			_, hasOrgInvite := sess.Get(ctx, session.KeyOrgInviteID).(int32)
			returnURL, hasReturnURL := sess.Get(ctx, session.KeyReturnURL).(string)
			if !hasOrgInvite && (!hasReturnURL || (len(returnURL) == 0) || (returnURL == "/") || (returnURL == rootRedirectURL)) {
				// we could redirect to create first widget on non-EE codepath, but non-EE is designed for only 1 user in mind
				newRegistrationRedirectURL = fmt.Sprintf("%s?%s=true", rootRedirectURL, common.ParamOnboarding)
			}
		} else {
			slog.ErrorContext(ctx, "Failed to complete registration", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
	}

	job := s.Jobs.LoginUser(sess)
	jobCtx := common.CopyTraceID(ctx, context.Background())
	if ip := ctx.Value(common.RateLimitKeyContextKey); ip != nil {
		jobCtx = context.WithValue(jobCtx, common.RateLimitKeyContextKey, ip)
	}
	go common.RunOneOffJob(jobCtx, job, job.NewParams())

	_ = sess.Set(ctx, session.KeyLoginStep, loginStepCompleted)
	_ = sess.Delete(ctx, session.KeyTwoFactorCode)
	_ = sess.Delete(ctx, session.KeyTwoFactorCodeTimestamp)
	_ = sess.Delete(ctx, session.KeyUserEmail)
	_ = sess.Set(ctx, session.KeyPersistent, true)

	if orgInviteID, ok := sess.Get(ctx, session.KeyOrgInviteID).(int32); ok && (orgInviteID > 0) {
		slog.DebugContext(ctx, "Found org invite ID in session, redirecting to org", "inviteID", orgInviteID)
		_ = sess.Delete(ctx, session.KeyOrgInviteID)
		// we can only rely on cache because if the user is redirected to portal root, they still can join the org later
		if invite, err := s.Store.Impl().GetCachedOrgInviteByID(ctx, orgInviteID); err == nil {
			redirectURL := s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(invite.OrgID)))
			common.Redirect(redirectURL, http.StatusOK, w, r)
			return
		}
		slog.WarnContext(ctx, "Org invite is not cached, redirecting to root", "inviteID", orgInviteID)
	}

	if len(newRegistrationRedirectURL) > 0 {
		_ = sess.Delete(ctx, session.KeyReturnURL)
		common.Redirect(newRegistrationRedirectURL, http.StatusOK, w, r)
	} else if returnURL, ok := sess.Get(ctx, session.KeyReturnURL).(string); ok && (len(returnURL) > 0) {
		slog.DebugContext(ctx, "Found return URL in user session", "url", returnURL)
		_ = sess.Delete(ctx, session.KeyReturnURL)
		common.Redirect(returnURL, http.StatusOK, w, r)
	} else {
		common.Redirect(rootRedirectURL, http.StatusOK, w, r)
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

	code := twoFactorCode(ctx)
	location := r.Header.Get(s.CountryCodeHeader.Value())

	if err := s.Mailer.SendTwoFactor(ctx, email, code, r.UserAgent(), location, false); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.render(w, r, "login/resend-error.html", renderContextNothing)
		return
	}

	_ = sess.Set(ctx, session.KeyTwoFactorCode, code)
	_ = sess.Set(ctx, session.KeyTwoFactorCodeTimestamp, time.Now().UTC())
	s.render(w, r, "login/resend.html", renderContextNothing)
}

// parseOrgInviteIDFromURL extracts org invite ID from a URL path like /orginvite/{id}/signup
func (s *Server) parseOrgInviteIDFromURL(rawURL string) int32 {
	// URL pattern: /orginvite/{encoded_id}/signup
	prefix := "/" + common.OrgInviteEndpoint + "/"
	suffix := "/" + common.RegisterEndpoint

	idx := strings.Index(rawURL, prefix)
	if idx < 0 {
		return -1
	}

	// Extract the path after the prefix
	path := rawURL[idx+len(prefix):]

	// Find where the path segment ends
	endIdx := strings.Index(path, suffix)
	if endIdx <= 0 {
		return -1
	}

	idStr := path[:endIdx]
	inviteID, err := s.IDHasher.Decrypt(idStr)
	if err != nil || inviteID <= 0 {
		return -1
	}

	if inviteID > math.MaxInt32 {
		return -1
	}

	return int32(inviteID)
}
