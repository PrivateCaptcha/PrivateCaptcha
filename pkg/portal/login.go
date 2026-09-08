package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
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
	errPortalPropertyNotFound    = errors.New("portal property not found")
	errCaptchaSolutionMissing    = errors.New("captcha solution missing")
	errCaptchaVerificationFailed = errors.New("captcha verification failed")
)

type loginRenderContext struct {
	CsrfRenderContext
	CaptchaRenderContext
	Email         string
	EmailError    string
	CodeError     string
	NameError     string
	InviteID      string
	CanRegister   bool
	IsRegister    bool
	EmailReadonly bool
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
		View:  loginTemplate,
		IsNew: true,
	}, nil
}

func (s *Server) verifyPortalCaptcha(ctx context.Context, solution, sitekey string) error {
	if solution == "" {
		slog.WarnContext(ctx, "Captcha solution field is empty")
		return errCaptchaSolutionMissing
	}

	payload, err := s.PuzzleEngine.ParseSolutionPayload(ctx, []byte(solution))
	if err != nil {
		return err
	}

	ownerSource := &portalPropertyOwnerSource{Store: s.Store, Sitekey: sitekey}
	verifyResult, err := s.PuzzleEngine.Verify(ctx, payload, ownerSource, time.Now().UTC())
	if err != nil {
		slog.ErrorContext(ctx, "Failed to verify captcha due to internal error", common.ErrAttr(err))
		return err
	}
	if !verifyResult.Success() {
		slog.ErrorContext(ctx, "Failed to verify captcha", "errors", verifyResult.Error.String())
		return errCaptchaVerificationFailed
	}

	return nil
}

func (s *Server) lookupLoginUser(ctx context.Context, email string, data *loginRenderContext) (*dbgen.User, bool) {
	if err := checkmail.ValidateFormat(email); err != nil {
		slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
		data.EmailError = "Email address is not valid."
		return nil, false
	}

	user, err := s.Store.Impl().FindUserByEmail(ctx, email)
	if err == nil {
		return user, true
	}

	switch err {
	case db.ErrDisabled:
		slog.WarnContext(ctx, "Disabled user attempted to login", "email", email)
		data.EmailError = "This account has been disabled."
		return nil, false
	case db.ErrSoftDeleted:
		slog.WarnContext(ctx, "Soft-deleted user attempted to login", "email", email)
	default:
		slog.WarnContext(ctx, "Failed to find active user by email", "email", email, common.ErrAttr(err))
	}
	data.EmailError = "User with such email does not exist."
	return nil, false
}

func (s *Server) startSignIn(w http.ResponseWriter, r *http.Request, user *dbgen.User, email string) (session.Authority, bool) {
	ctx := r.Context()

	sess, err := s.Sessions.Start(w, r)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to start sign-in session", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return session.Authority{}, false
	}
	if authority, ok := sess.Authority(); ok && authority.State == session.StateAuthenticated {
		slog.DebugContext(ctx, "User is already logged in", "email", email)
		common.Redirect(s.RelURL("/"), http.StatusOK, w, r)
		return session.Authority{}, false
	}
	if err := sess.Set(ctx, session.KeyUserName, user.Name); err != nil {
		slog.ErrorContext(ctx, "Failed to set sign-in Payload", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return session.Authority{}, false
	}

	code := twoFactorCode(ctx)
	location := r.Header.Get(s.CountryCodeHeader.Value())
	result, err := s.Sessions.IssueSignInChallenge(w, r, sess, user.ID, fmt.Sprintf("%06d", code), s.TwoFactorDuration, maxFailedAttempts)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to issue sign-in challenge", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return session.Authority{}, false
	}
	issuedAuthority, ok := challengeResultAuthority(result, session.ChallengeKindSignIn)
	if !ok {
		var outcome session.TransitionOutcome
		if result != nil {
			outcome = result.Outcome
		}
		if outcome == session.TransitionAttemptsExhausted {
			s.Sessions.ClearCookie(w, r)
		}
		slog.WarnContext(ctx, "Sign-in challenge was not issued", "outcome", outcome)
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		return session.Authority{}, false
	}
	if err := s.Mailer.SendTwoFactor(ctx, issuedAuthority.ChallengeEmail, code, r.UserAgent(), location, false); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return session.Authority{}, false
	}

	return issuedAuthority, true
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
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

	if err := s.verifyPortalCaptcha(ctx, r.FormValue(common.ParamPortalSolution), data.CaptchaSitekey); err != nil {
		data.CaptchaError = captchaVerificationFailed
		if errors.Is(err, errCaptchaSolutionMissing) {
			data.CaptchaError = "You need to solve captcha to login."
		}
		s.render(w, r, loginContentsTemplate, data, false /*new*/)
		return
	}

	email := strings.TrimSpace(r.FormValue(common.ParamEmail))
	user, ok := s.lookupLoginUser(ctx, email, data)
	if !ok {
		s.render(w, r, loginContentsTemplate, data, false /*new*/)
		return
	}

	issuedAuthority, ok := s.startSignIn(w, r, user, email)
	if !ok {
		return
	}
	data.Token = s.XSRF.Token(issuedAuthority.ChallengeEmail)
	data.Email = common.MaskEmail(issuedAuthority.ChallengeEmail, '*')

	s.render(w, r, twofactorContentsTemplate, data, true /*new*/)
}
