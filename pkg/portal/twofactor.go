package portal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	renderContextNothing = struct{}{}
)

const maxFailedAttempts = 5

func challengeResultAuthority(result *session.ChallengeResult, kind session.ChallengeKind) (session.Authority, bool) {
	if result == nil || result.Outcome != session.TransitionSucceeded || result.Session == nil {
		return session.Authority{}, false
	}
	return result.Authority, result.Authority.ChallengeKind == kind && result.Authority.ChallengeEmail != ""
}

func (s *Server) handleChallengeOutcome(w http.ResponseWriter, r *http.Request, data *loginRenderContext, outcome session.TransitionOutcome) bool {
	switch outcome {
	case session.TransitionSucceeded:
		return false
	case session.TransitionInvalidCode:
		data.CodeError = "Code is not valid."
	case session.TransitionAttemptsExhausted:
		data.CodeError = "Too many failed attempts. Please start again."
	case session.TransitionVerificationRequired:
		common.Redirect(s.RelURL(common.AccountVerifyEndpoint), http.StatusUnauthorized, w, r)
		return true
	default:
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return true
	}
	s.render(w, r, "login/twofactor-form.html", data, false /*new*/)
	return true
}

func (s *Server) postTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	sess, err := s.Sessions.Get(r)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve pending session", common.ErrAttr(err))
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}
	ctx = context.WithValue(ctx, common.SessionHashContextKey, sess.Hash())
	authority, ok := sess.Authority()
	if !ok || authority.State != session.StatePending || authority.ChallengeEmail == "" {
		slog.WarnContext(ctx, "User session is not pending")
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	data := &loginRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(authority.ChallengeEmail),
		},
		Email: common.MaskEmail(authority.ChallengeEmail, '*'),
	}
	formCode := strings.TrimSpace(r.FormValue(common.ParamVerificationCode))
	var newRegistrationRedirectURL string
	var orgInviteID int32
	rootRedirectURL := s.RelURL("/")

	switch authority.ChallengeKind {
	case session.ChallengeKindSignIn:
		result, err := s.Sessions.ConsumeSignInChallenge(w, r, sess, formCode, maxFailedAttempts)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to consume sign-in challenge", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
		if result != nil && result.Outcome == session.TransitionAttemptsExhausted {
			s.Sessions.ClearCookie(w, r)
		}
		if result == nil || s.handleChallengeOutcome(w, r, data, result.Outcome) {
			return
		}
		sess = result.Session
	case session.ChallengeKindRegistration:
		if !s.verifyCSRF(w, r, authority.ChallengeEmail) {
			return
		}
		result, err := s.Sessions.ConsumeRegistrationChallenge(ctx, sess, formCode, maxFailedAttempts)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to consume registration challenge", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
		if result != nil && result.Outcome == session.TransitionAttemptsExhausted {
			s.Sessions.ClearCookie(w, r)
		}
		if result == nil || s.handleChallengeOutcome(w, r, data, result.Outcome) {
			return
		}
		user, _, err := s.doRegister(ctx, result.Email, result.Name, result.InviteID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to complete registration", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
		if err := sess.Set(ctx, session.KeyFirstSession, true); err != nil {
			slog.ErrorContext(ctx, "Failed to set registration successor Payload", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
		successor, err := s.Sessions.CreateRegistrationSuccessor(w, r, sess, user.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create registration successor", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
		if successor == nil || successor.Outcome != session.TransitionSucceeded || successor.Session == nil {
			common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
			return
		}
		sess = successor.Session
		orgInviteID = result.InviteID
		returnURL, hasReturnURL := sess.Get(ctx, session.KeyReturnURL).(string)
		if orgInviteID <= 0 && (!hasReturnURL || returnURL == "" || returnURL == "/" || returnURL == rootRedirectURL) {
			newRegistrationRedirectURL = fmt.Sprintf("%s?%s=true", rootRedirectURL, common.ParamOnboarding)
		}
	default:
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	ctx = context.WithValue(ctx, common.SessionHashContextKey, sess.Hash())

	job := s.Jobs.LoginUser(sess)
	jobCtx := common.CopyTraceID(ctx, context.Background())
	jobCtx = context.WithValue(jobCtx, common.SessionHashContextKey, sess.Hash())
	if ip := ctx.Value(common.RateLimitKeyContextKey); ip != nil {
		jobCtx = context.WithValue(jobCtx, common.RateLimitKeyContextKey, ip)
	}
	go common.RunOneOffJob(jobCtx, job, job.NewParams())

	if orgInviteID <= 0 {
		orgInviteID, _ = sess.Get(ctx, session.KeyOrgInviteID).(int32)
	}
	if orgInviteID > 0 {
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

	sess, err := s.Sessions.Get(r)
	if err != nil {
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}
	authority, ok := sess.Authority()
	if !ok || authority.State != session.StatePending || authority.ChallengeEmail == "" {
		slog.WarnContext(ctx, "User session is not pending")
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	code := twoFactorCode(ctx)
	location := r.Header.Get(s.CountryCodeHeader.Value())
	result, err := s.Sessions.ResendPendingChallenge(ctx, sess, fmt.Sprintf("%06d", code), s.TwoFactorDuration, maxFailedAttempts)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to resend pending challenge", common.ErrAttr(err))
		s.render(w, r, "login/resend-error.html", renderContextNothing, false /*new*/)
		return
	}
	if result != nil && result.Outcome == session.TransitionAttemptsExhausted {
		s.Sessions.ClearCookie(w, r)
	}
	resentAuthority, ok := challengeResultAuthority(result, authority.ChallengeKind)
	if !ok {
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}
	if err := s.Mailer.SendTwoFactor(ctx, resentAuthority.ChallengeEmail, code, r.UserAgent(), location, resentAuthority.ChallengeKind == session.ChallengeKindRegistration); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.render(w, r, "login/resend-error.html", renderContextNothing, false /*new*/)
		return
	}

	s.render(w, r, "login/resend.html", renderContextNothing, false /*new*/)
}
