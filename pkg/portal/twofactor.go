package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	renderContextNothing = struct{}{}
)

const maxFailedAttempts = 5

func (s *Server) postTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	sess, err := s.Sessions.SessionGetChecked(r)
	if err != nil {
		if errors.Is(err, session.ErrSessionMissing) {
			common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		} else {
			slog.ErrorContext(ctx, "Failed to load sign-in challenge session", common.ErrAttr(err))
			s.RedirectError(http.StatusServiceUnavailable, w, r)
		}
		return
	}
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

	data := &loginRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(sess.ID()),
		},
		Email: common.MaskEmail(email, '*'),
	}

	formCode := strings.TrimSpace(r.FormValue(common.ParamVerificationCode))
	var newRegistrationRedirectURL string
	rootRedirectURL := s.RelURL("/")
	encodedCode := s.encodeVerificationCode(formCode)
	if encodedCode == "" {
		data.CodeError = verificationCodeError(false)
		s.render(w, r, "login/twofactor-form.html", data, false /*new*/)
		return
	}

	if step == loginStepSignInVerify {
		renewed, result, err := s.Sessions.SessionConsumeSignInChallengeBlocking(w, r, sess, encodedCode, maxFailedAttempts, loginStepCompleted)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to consume sign-in challenge", common.ErrAttr(err))
			s.RedirectError(http.StatusServiceUnavailable, w, r)
			return
		}
		if !result.Consumed {
			data.CodeError = verificationCodeError(result.AttemptsExhausted)
			slog.WarnContext(ctx, "Sign-in code verification failed", "attemptsExhausted", result.AttemptsExhausted)
			s.render(w, r, "login/twofactor-form.html", data, false /*new*/)
			return
		}
		sess = renewed
	} else {
		requiresVerification, _ := sess.Get(ctx, session.KeyVerifyRegistration).(bool)
		processing, result, err := s.Sessions.SessionConsumeRegistrationChallengeBlocking(r, sess, encodedCode, maxFailedAttempts, !requiresVerification, loginStepCompleted)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to consume registration challenge", common.ErrAttr(err))
			s.RedirectError(http.StatusServiceUnavailable, w, r)
			return
		}
		if result.Verified && requiresVerification {
			slog.WarnContext(ctx, "Account requires an additional verification", "email", email)
			common.Redirect(s.RelURL(common.AccountVerifyEndpoint), http.StatusUnauthorized, w, r)
			return
		}
		if !result.Consumed {
			data.CodeError = verificationCodeError(result.AttemptsExhausted)
			slog.WarnContext(ctx, "Registration code verification failed", "attemptsExhausted", result.AttemptsExhausted)
			s.render(w, r, "login/twofactor-form.html", data, false /*new*/)
			return
		}
		sess = processing

		slog.DebugContext(ctx, "Proceeding with the user registration flow after 2FA")
		if user, _, err := s.doRegister(ctx, sess); err == nil {
			_, hasOrgInvite := sess.Get(ctx, session.KeyOrgInviteID).(int32)
			returnURL, hasReturnURL := sess.Get(ctx, session.KeyReturnURL).(string)
			if !hasOrgInvite && (!hasReturnURL || (len(returnURL) == 0) || (returnURL == "/") || (returnURL == rootRedirectURL)) {
				// we could redirect to create first widget on non-EE codepath, but non-EE is designed for only 1 user in mind
				newRegistrationRedirectURL = fmt.Sprintf("%s?%s=true", rootRedirectURL, common.ParamOnboarding)
			}
			if err := s.Sessions.SessionAuthenticateRegistration(w, r, sess, user.ID, loginStepCompleted); err != nil {
				slog.ErrorContext(ctx, "Failed to authenticate registration session locally", common.ErrAttr(err))
				s.RedirectError(http.StatusInternalServerError, w, r)
				return
			}
			job := s.Jobs.FinalizeRegistration(sess, user.ID)
			jobCtx := common.CopyTraceID(ctx, context.Background())
			jobCtx = context.WithValue(jobCtx, common.SessionIDContextKey, sess.ID())
			go common.RunOneOffJob(jobCtx, job, job.NewParams())
		} else {
			slog.ErrorContext(ctx, "Failed to complete registration", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
	}
	ctx = context.WithValue(ctx, common.SessionIDContextKey, sess.ID())

	job := s.Jobs.LoginUser(sess)
	jobCtx := common.CopyTraceID(ctx, context.Background())
	jobCtx = context.WithValue(jobCtx, common.SessionIDContextKey, sess.ID())
	if ip := ctx.Value(common.RateLimitKeyContextKey); ip != nil {
		jobCtx = context.WithValue(jobCtx, common.RateLimitKeyContextKey, ip)
	}
	go common.RunOneOffJob(jobCtx, job, job.NewParams())

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

	sess, err := s.Sessions.SessionGetChecked(r)
	if err != nil {
		if errors.Is(err, session.ErrSessionMissing) {
			common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		} else {
			slog.ErrorContext(ctx, "Failed to load resend challenge session", common.ErrAttr(err))
			s.RedirectError(http.StatusServiceUnavailable, w, r)
		}
		return
	}
	step, ok := sess.Get(ctx, session.KeyLoginStep).(int)
	if !ok || ((step != loginStepSignInVerify) && (step != loginStepSignUpVerify)) {
		slog.WarnContext(ctx, "User session is not valid", "step", step)
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	code := twoFactorCode(ctx)
	fallbackCode := twoFactorCode(ctx)
	for fallbackCode == code {
		fallbackCode = twoFactorCode(ctx)
	}
	encodedCode := s.IDHasher.Encrypt(code)
	fallbackEncodedCode := s.IDHasher.Encrypt(fallbackCode)
	var reissuedCode, email, challengeType string
	var reissueErr error
	if step == loginStepSignInVerify {
		challengeType = "sign-in"
		reissued, err := sess.ReissueSignInChallenge(ctx, encodedCode, fallbackEncodedCode, s.TwoFactorDuration)
		reissuedCode, email = reissued.EncodedCode, reissued.Email
		reissueErr = err
	} else {
		challengeType = "registration"
		reissued, err := sess.ReissueRegistrationChallenge(ctx, encodedCode, fallbackEncodedCode, s.TwoFactorDuration)
		reissuedCode, email = reissued.EncodedCode, reissued.Email
		reissueErr = err
	}
	if reissueErr != nil || reissuedCode == "" || email == "" {
		slog.ErrorContext(ctx, "Failed to reissue challenge", "type", challengeType, common.ErrAttr(reissueErr))
		s.render(w, r, "login/resend-error.html", renderContextNothing, false /*new*/)
		return
	}
	code, err = s.IDHasher.Decrypt(reissuedCode)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode reissued challenge", "type", challengeType, common.ErrAttr(err))
		s.render(w, r, "login/resend-error.html", renderContextNothing, false /*new*/)
		return
	}

	location := r.Header.Get(s.CountryCodeHeader.Value())
	if err := s.Mailer.SendTwoFactor(ctx, email, code, r.UserAgent(), location, false); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.render(w, r, "login/resend-error.html", renderContextNothing, false /*new*/)
		return
	}
	s.render(w, r, "login/resend.html", renderContextNothing, false /*new*/)
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
