package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

func (s *Server) postRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if !s.canRegister.Load() {
		slog.WarnContext(ctx, "Registration is disabled")
		s.RedirectError(http.StatusNotImplemented, w, r)
		return
	}

	email := strings.TrimSpace(r.FormValue(common.ParamEmail))

	data := &loginRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		CaptchaRenderContext: s.CreateCaptchaRenderContext(db.PortalRegisterSitekey),
		Email:                email,
		IsRegister:           true,
	}

	if _, termsAndConditions := r.Form[common.ParamTerms]; !termsAndConditions {
		// it's an error because they are marked 'required' on the frontend, so something went terribly wrong
		slog.ErrorContext(ctx, "Terms and conditions were not accepted")
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	captchaSolution := r.FormValue(common.ParamPortalSolution)
	if len(captchaSolution) == 0 {
		slog.WarnContext(ctx, "Captcha solution field is empty")
		data.CaptchaError = "You need to solve captcha to register."
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	payload, err := s.PuzzleEngine.ParseSolutionPayload(ctx, []byte(captchaSolution))
	if err != nil {
		data.CaptchaError = captchaVerificationFailed
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	ownerSource := &portalPropertyOwnerSource{Store: s.Store, Sitekey: data.CaptchaSitekey}
	verifyResult, err := s.PuzzleEngine.Verify(ctx, payload, ownerSource, time.Now().UTC())
	if err != nil {
		slog.ErrorContext(ctx, "Failed to verify captcha due to internal error", common.ErrAttr(err))
		data.CaptchaError = captchaVerificationFailed
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}
	if !verifyResult.Success() {
		slog.ErrorContext(ctx, "Failed to verify captcha", "errors", verifyResult.Error.String())
		data.CaptchaError = captchaVerificationFailed
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	name := strings.TrimSpace(r.FormValue(common.ParamName))
	if len(name) < 3 {
		data.NameError = "Please use a longer name."
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	if !isUserNameValid(name) {
		data.NameError = userNameErrorMessage
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	if err := s.EmailVerifier.VerifyEmail(ctx, email); err != nil {
		slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
		data.Email = ""
		data.EmailError = "Email address is not valid."
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	if _, err := s.Store.Impl().FindUserByEmail(ctx, email); err == nil {
		slog.WarnContext(ctx, "User with such email already exists", "email", email)
		data.Email = ""
		data.EmailError = emailAlreadyRegisteredError
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	} else if errors.Is(err, db.ErrDisabled) || errors.Is(err, db.ErrSoftDeleted) {
		slog.WarnContext(ctx, "User is already registered but unavailable", "email", email, common.ErrAttr(err))
		data.Email = ""
		data.EmailError = accountUnavailableError
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	code := twoFactorCode(ctx)
	location := r.Header.Get(s.CountryCodeHeader.Value())

	var existing *session.Session
	if current, ok := s.Sessions.SessionGet(r); ok {
		existing = current
	}
	sess, err := s.Sessions.SessionPrepare(r)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to prepare registration session", common.ErrAttr(err))
		s.RedirectError(http.StatusServiceUnavailable, w, r)
		return
	}
	if existing != nil {
		sess.Merge(existing)
	}

	// Validate email matches invited email if this is an invite registration
	if inviteID, ok := sess.Get(ctx, session.KeyOrgInviteID).(int32); ok && inviteID > 0 {
		if invite, err := s.Store.Impl().GetCachedOrgInviteByID(ctx, inviteID); err == nil {
			if !strings.EqualFold(email, invite.Email.String) {
				data.EmailError = fmt.Sprintf("You must register with %s to accept this organization invitation.", invite.Email.String)
				data.Email = invite.Email.String
				data.EmailReadonly = true
				s.render(w, r, registerContentsTemplate, data, false /*new*/)
				return
			}
		}
	}
	ctx = context.WithValue(ctx, common.SessionIDContextKey, sess.ID())
	_ = sess.Set(ctx, session.KeyLoginStep, loginStepSignUpVerify)
	_ = sess.Set(ctx, session.KeyUserEmail, email)
	_ = sess.Set(ctx, session.KeyUserName, name)
	_ = sess.Delete(ctx, session.KeyVerifyRegistration)
	checkJob := s.Jobs.CheckRegistration(sess, r)
	if err := checkJob.RunOnce(ctx, checkJob.NewParams()); err != nil {
		slog.ErrorContext(ctx, "Failed to check registration", common.ErrAttr(err))
		s.RedirectError(http.StatusServiceUnavailable, w, r)
		return
	}

	if err := s.Mailer.SendTwoFactor(ctx, email, code, r.UserAgent(), location, true); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		data.EmailError = "Failed to send a confirmation email. Please try again."
		s.render(w, r, registerContentsTemplate, data, false /*new*/)
		return
	}

	if err := sess.PersistRegistrationChallenge(ctx, s.IDHasher.Encrypt(code), email, s.TwoFactorDuration); err != nil {
		slog.ErrorContext(ctx, "Failed to persist registration session", common.ErrAttr(err))
		s.RedirectError(http.StatusServiceUnavailable, w, r)
		return
	}
	s.Sessions.SessionCommit(w, r, sess)

	data.Token = s.XSRF.Token(sess.ID())
	data.Email = common.MaskEmail(email, '*')

	slog.DebugContext(ctx, "Started 2FA registration flow", "email", email)

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

func (s *Server) doRegister(ctx context.Context, sess *session.Session) (*dbgen.User, *dbgen.Organization, error) {
	email, ok := sess.Get(ctx, session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		return nil, nil, errIncompleteSession
	}

	name, ok := sess.Get(ctx, session.KeyUserName).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get user name from session")
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

	// Check for org invite ID in session (optional)
	var orgInviteID *int32
	if inviteID, ok := sess.Get(ctx, session.KeyOrgInviteID).(int32); ok && inviteID > 0 {
		orgInviteID = &inviteID
	}

	job := s.Jobs.OnboardUser(user, plan, orgInviteID)
	go common.RunOneOffJob(common.CopyTraceID(ctx, context.Background()), job, job.NewParams())

	return user, org, nil
}
