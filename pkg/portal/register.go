package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	errIncompleteSession    = errors.New("data in session is incomplete")
	errRegistrationDisabled = errors.New("registration disabled")
)

const (
	registerContentsTemplate    = "login/register-contents.html"
	userNameErrorMessage        = "Name contains invalid characters."
	emailAlreadyRegisteredError = "Such email is already registered. Login instead?"
	accountUnavailableError     = "Such email already belongs to an inactive account."
	accountVerifyTemplate       = "account-verify/verification.html"
)

func (s *Server) getRegister(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	if !s.canRegister.Load() {
		return nil, errRegistrationDisabled
	}

	return &ViewModel{
		Model: &loginRenderContext{
			CsrfRenderContext: CsrfRenderContext{
				Token: s.XSRF.Token(""),
			},
			CaptchaRenderContext: s.CreateCaptchaRenderContext(db.PortalRegisterSitekey),
			IsRegister:           true,
		},
		View:  loginTemplate,
		IsNew: true,
	}, nil
}

func (s *Server) getAccountVerify(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	if !s.canRegister.Load() {
		return nil, errRegistrationDisabled
	}

	return &ViewModel{
		Model: &loginRenderContext{},
		View:  accountVerifyTemplate,
		IsNew: true,
	}, nil
}

func isUserNameValid(name string) bool {
	if len(name) == 0 {
		return false
	}

	const allowedPunctuation = "'-"

	for _, r := range name {
		switch {
		case unicode.IsLetter(r):
			continue
		case unicode.IsSpace(r):
			continue
		case strings.ContainsRune(allowedPunctuation, r):
			continue
		default:
			return false
		}
	}

	return true
}

type registrationInput struct {
	email           string
	name            string
	inviteID        int32
	encodedInviteID string
}

type issuedRegistrationChallenge struct {
	authority session.Authority
	sess      *session.Session
	code      int
	location  string
}

func (s *Server) parseRegistrationInput(r *http.Request) (registrationInput, bool) {
	input := registrationInput{
		email:           strings.TrimSpace(r.FormValue(common.ParamEmail)),
		name:            strings.TrimSpace(r.FormValue(common.ParamName)),
		encodedInviteID: r.FormValue(common.ParamID),
	}
	if input.encodedInviteID == "" {
		return input, true
	}

	value, err := s.IDHasher.Decrypt(input.encodedInviteID)
	if err != nil || value <= 0 || value > math.MaxInt32 {
		slog.WarnContext(r.Context(), "Invalid invite ID in registration form", "idStr", input.encodedInviteID, common.ErrAttr(err))
		return registrationInput{}, false
	}
	input.inviteID = int32(value)
	return input, true
}

func (s *Server) validateRegistrationInput(ctx context.Context, input registrationInput, data *loginRenderContext) bool {
	if len(input.name) < 3 {
		data.NameError = "Please use a longer name."
		return false
	}

	if !isUserNameValid(input.name) {
		data.NameError = userNameErrorMessage
		return false
	}

	if err := s.EmailVerifier.VerifyEmail(ctx, input.email); err != nil {
		slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
		data.Email = ""
		data.EmailError = "Email address is not valid."
		return false
	}

	if _, err := s.Store.Impl().FindUserByEmail(ctx, input.email); err == nil {
		slog.WarnContext(ctx, "User with such email already exists", "email", input.email)
		data.Email = ""
		data.EmailError = emailAlreadyRegisteredError
		return false
	} else if errors.Is(err, db.ErrDisabled) || errors.Is(err, db.ErrSoftDeleted) {
		slog.WarnContext(ctx, "User is already registered but unavailable", "email", input.email, common.ErrAttr(err))
		data.Email = ""
		data.EmailError = accountUnavailableError
		return false
	}

	return true
}

func (s *Server) issueRegistrationChallenge(w http.ResponseWriter, r *http.Request, input registrationInput) (*issuedRegistrationChallenge, bool) {
	ctx := r.Context()
	code := twoFactorCode(ctx)
	location := r.Header.Get(s.CountryCodeHeader.Value())

	sess, err := s.Sessions.Start(w, r)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to start registration session", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return nil, false
	}
	if err := sess.Set(ctx, session.KeyUserName, input.name); err != nil {
		slog.ErrorContext(ctx, "Failed to set registration Payload", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return nil, false
	}
	result, err := s.Sessions.IssueRegistrationChallenge(w, r, sess, input.inviteID, input.email, fmt.Sprintf("%06d", code),
		s.TwoFactorDuration, maxFailedAttempts)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to issue registration challenge", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return nil, false
	}
	issuedAuthority, ok := challengeResultAuthority(result, session.ChallengeKindRegistration)
	if !ok {
		var outcome session.TransitionOutcome
		if result != nil {
			outcome = result.Outcome
		}
		if outcome == session.TransitionAttemptsExhausted {
			s.Sessions.ClearCookie(w, r)
		}
		slog.WarnContext(ctx, "Registration challenge was not issued", "outcome", outcome)
		common.Redirect(s.RelURL(common.RegisterEndpoint), http.StatusUnauthorized, w, r)
		return nil, false
	}
	job := s.Jobs.CheckRegistration(result.Session, r, input.inviteID)
	jobCtx := common.CopyTraceID(ctx, context.Background())
	if ip := ctx.Value(common.RateLimitKeyContextKey); ip != nil {
		jobCtx = context.WithValue(jobCtx, common.RateLimitKeyContextKey, ip)
	}
	go common.RunOneOffJob(jobCtx, job, job.NewParams())

	return &issuedRegistrationChallenge{
		authority: issuedAuthority,
		sess:      result.Session,
		code:      code,
		location:  location,
	}, true
}

func (s *Server) postRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if !s.canRegister.Load() {
		slog.WarnContext(ctx, "Registration is disabled")
		s.RedirectError(http.StatusNotImplemented, w, r)
		return
	}

	input, ok := s.parseRegistrationInput(r)
	if !ok {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}
	data := &loginRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		CaptchaRenderContext: s.CreateCaptchaRenderContext(db.PortalRegisterSitekey),
		Email:                input.email,
		InviteID:             input.encodedInviteID,
		IsRegister:           true,
	}

	if _, termsAndConditions := r.Form[common.ParamTerms]; !termsAndConditions {
		// It's an error because the field is required on the frontend.
		slog.ErrorContext(ctx, "Terms and conditions were not accepted")
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if err := s.verifyPortalCaptcha(ctx, r.FormValue(common.ParamPortalSolution), data.CaptchaSitekey); err != nil {
		data.CaptchaError = captchaVerificationFailed
		if errors.Is(err, errCaptchaSolutionMissing) {
			data.CaptchaError = "You need to solve captcha to register."
		}
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	if !s.validateRegistrationInput(ctx, input, data) {
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	challenge, ok := s.issueRegistrationChallenge(w, r, input)
	if !ok {
		return
	}
	if err := s.Mailer.SendTwoFactor(ctx, challenge.authority.ChallengeEmail, challenge.code, r.UserAgent(), challenge.location, true); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		data.EmailError = "Failed to send a confirmation email. Please try again."
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	ctx = context.WithValue(ctx, common.SessionHashContextKey, challenge.sess.Hash())

	data.Token = s.XSRF.Token(challenge.authority.ChallengeEmail)
	data.Email = common.MaskEmail(challenge.authority.ChallengeEmail, '*')

	slog.DebugContext(ctx, "Started 2FA registration flow", "email", input.email)

	s.render(w, r, twofactorContentsTemplate, data, true /*new*/)
}

func createInternalTrial(plan billing.Plan, status string) *dbgen.CreateSubscriptionParams {
	priceIDMonthly, priceIDYearly := plan.PriceIDs()
	priceID := priceIDMonthly
	if len(priceID) == 0 {
		priceID = priceIDYearly
	}
	return &dbgen.CreateSubscriptionParams{
		ExternalProductID:      plan.ProductID(),
		ExternalPriceID:        priceID,
		ExternalSubscriptionID: pgtype.Text{},
		ExternalCustomerID:     pgtype.Text{},
		Status:                 status,
		Source:                 dbgen.SubscriptionSourceInternal,
		TrialEndsAt:            db.Timestampz(time.Now().AddDate(0, 0, plan.TrialDays())),
		NextBilledAt:           db.Timestampz(time.Time{}),
	}
}

func (s *Server) doRegister(ctx context.Context, email, name string, inviteID int32) (*dbgen.User, *dbgen.Organization, error) {
	if email == "" || name == "" {
		return nil, nil, errIncompleteSession
	}
	plan := s.PlanService.GetInternalTrialPlan()
	subscrParams := createInternalTrial(plan, s.PlanService.ActiveTrialStatus())

	var user *dbgen.User
	var org *dbgen.Organization

	if auditEvents, err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		var err error
		var auditEvents []*common.AuditLogEvent
		user, org, auditEvents, err = impl.CreateNewAccount(ctx, subscrParams, email, name, common.DefaultOrgName, -1 /*existing user ID*/)
		return auditEvents, err
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to create user account in Store", common.ErrAttr(err))
		return nil, nil, err
	} else {
		s.Store.AuditLog().RecordEvents(ctx, auditEvents, common.AuditLogSourcePortal)
	}

	job := s.Jobs.OnboardUser(user, plan)
	go common.RunOneOffJob(common.CopyTraceID(ctx, context.Background()), job, job.NewParams())

	return user, org, nil
}
