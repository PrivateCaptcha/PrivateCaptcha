package portal

import (
	"context"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	renderContextNothing = struct{}{}
)

const maxFailedAttempts = 5

type twoFactorCompletion struct {
	sess                    *session.Session
	orgInviteID             int32
	registrationRedirectURL string
}

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
		s.Sessions.ClearCookie(w, r)
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

func (s *Server) resolvePendingSession(w http.ResponseWriter, r *http.Request) (*session.Session, session.Authority, bool) {
	ctx := r.Context()

	sess, err := s.Sessions.Get(r)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve pending session", common.ErrAttr(err))
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return nil, session.Authority{}, false
	}
	ctx = context.WithValue(ctx, common.SessionHashContextKey, sess.Hash())
	authority, ok := sess.Authority()
	if !ok || authority.State != session.StatePending || authority.ChallengeEmail == "" {
		slog.WarnContext(ctx, "User session is not pending")
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return nil, session.Authority{}, false
	}

	return sess, authority, true
}

func (s *Server) completeRegistrationChallenge(
	w http.ResponseWriter,
	r *http.Request,
	sess *session.Session,
	authority session.Authority,
) (*twoFactorCompletion, bool) {
	ctx := context.WithValue(r.Context(), common.SessionHashContextKey, sess.Hash())
	data := &loginRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(authority.ChallengeEmail),
		},
		Email: common.MaskEmail(authority.ChallengeEmail, '*'),
	}
	formCode := strings.TrimSpace(r.FormValue(common.ParamVerificationCode))
	if !s.verifyCSRF(w, r, authority.ChallengeEmail) {
		return nil, false
	}

	result, err := s.Sessions.ConsumeRegistrationChallenge(ctx, sess, formCode, maxFailedAttempts)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to consume registration challenge", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return nil, false
	}
	if result == nil || s.handleChallengeOutcome(w, r, data, result.Outcome) {
		return nil, false
	}

	user, _, err := s.doRegister(ctx, result.Email, result.Name, result.InviteID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to complete registration", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return nil, false
	}
	if err := sess.Set(ctx, session.KeyFirstSession, true); err != nil {
		slog.ErrorContext(ctx, "Failed to set registration successor Payload", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return nil, false
	}

	successor, err := s.Sessions.CreateRegistrationSuccessor(w, r, sess, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create registration successor", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return nil, false
	}
	if successor == nil || successor.Outcome != session.TransitionSucceeded || successor.Session == nil {
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return nil, false
	}

	completion := &twoFactorCompletion{
		sess:        successor.Session,
		orgInviteID: result.InviteID,
	}
	rootRedirectURL := s.RelURL("/")
	returnURL, hasReturnURL := completion.sess.Get(ctx, session.KeyReturnURL).(string)
	if completion.orgInviteID <= 0 && (!hasReturnURL || returnURL == "" || returnURL == "/" || returnURL == rootRedirectURL) {
		completion.registrationRedirectURL = fmt.Sprintf("%s?%s=true", rootRedirectURL, common.ParamOnboarding)
	}

	return completion, true
}

func (s *Server) orgInviteRedirectURL(ctx context.Context, sess *session.Session, orgInviteID int32) string {
	slog.DebugContext(ctx, "Found org invite ID in session, redirecting to org", "inviteID", orgInviteID)
	_ = sess.Delete(ctx, session.KeyOrgInviteID)

	// we warmup the cache via registrationCheckJob so most of the time there should be no DB roundtrip
	invite, err := s.Store.Impl().RetrieveOrgInviteByID(ctx, orgInviteID)
	if err != nil {
		slog.WarnContext(ctx, "Org invite is not cached, redirecting to root", "inviteID", orgInviteID)
		return ""
	}

	user, userErr := s.SessionUser(ctx, sess)
	if userErr == nil && !invite.UserID.Valid {
		invite, err = s.Store.Impl().LinkOrgInviteToUser(ctx, orgInviteID, user)
	}
	if userErr == nil && err == nil && invite.UserID.Valid && invite.UserID.Int32 == user.ID {
		return s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(invite.OrgID)))
	}

	slog.WarnContext(ctx, "Org invite is not linked to user, redirecting to root", "inviteID", orgInviteID, "userError", userErr, "linkError", err)
	return ""
}

func (s *Server) postTwoFactor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	sess, authority, ok := s.resolvePendingSession(w, r)
	if !ok {
		return
	}
	ctx = context.WithValue(ctx, common.SessionHashContextKey, sess.Hash())

	completion := &twoFactorCompletion{sess: sess}
	switch authority.ChallengeKind {
	case session.ChallengeKindSignIn:
		data := &loginRenderContext{
			CsrfRenderContext: CsrfRenderContext{
				Token: s.XSRF.Token(authority.ChallengeEmail),
			},
			Email: common.MaskEmail(authority.ChallengeEmail, '*'),
		}
		formCode := strings.TrimSpace(r.FormValue(common.ParamVerificationCode))
		result, err := s.Sessions.ConsumeSignInChallenge(w, r, sess, formCode, maxFailedAttempts)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to consume sign-in challenge", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}
		if result == nil || s.handleChallengeOutcome(w, r, data, result.Outcome) {
			return
		}
		completion.sess = result.Session
	case session.ChallengeKindRegistration:
		completion, ok = s.completeRegistrationChallenge(w, r, sess, authority)
		if !ok {
			return
		}
	default:
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return
	}

	if lenTips := len(s.Tips); lenTips > 0 {
		_ = completion.sess.Set(ctx, session.KeyTip, randv2.IntN(lenTips))
	}

	ctx = context.WithValue(ctx, common.SessionHashContextKey, completion.sess.Hash())

	job := s.Jobs.LoginUser(completion.sess)
	jobCtx := common.CopyTraceID(ctx, context.Background())
	jobCtx = context.WithValue(jobCtx, common.SessionHashContextKey, completion.sess.Hash())
	if ip := ctx.Value(common.RateLimitKeyContextKey); ip != nil {
		jobCtx = context.WithValue(jobCtx, common.RateLimitKeyContextKey, ip)
	}
	go common.RunOneOffJob(jobCtx, job, job.NewParams())

	if completion.orgInviteID <= 0 {
		completion.orgInviteID, _ = completion.sess.Get(ctx, session.KeyOrgInviteID).(int32)
	}
	if completion.orgInviteID > 0 {
		redirectURL := s.orgInviteRedirectURL(ctx, completion.sess, completion.orgInviteID)
		if redirectURL != "" {
			common.Redirect(redirectURL, http.StatusOK, w, r)
			return
		}
	}

	rootRedirectURL := s.RelURL("/")
	if completion.registrationRedirectURL != "" {
		_ = completion.sess.Delete(ctx, session.KeyReturnURL)
		common.Redirect(completion.registrationRedirectURL, http.StatusOK, w, r)
	} else if returnURL, ok := completion.sess.Get(ctx, session.KeyReturnURL).(string); ok && returnURL != "" {
		slog.DebugContext(ctx, "Found return URL in user session", "url", returnURL)
		_ = completion.sess.Delete(ctx, session.KeyReturnURL)
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
