package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/badoux/checkmail"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	errIncompleteSession    = errors.New("data in session is incomplete")
	errRegistrationDisabled = errors.New("registration disabled")
)

const (
	registerFormTemplate = "register/form.html"
	registerTemplate     = "register/register.html"
)

type registerRenderContext struct {
	CsrfRenderContext
	CaptchaRenderContext
	NameError  string
	EmailError string
}

func (s *Server) getRegister(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	if !s.canRegister.Load() {
		return nil, "", errRegistrationDisabled
	}

	return &registerRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		CaptchaRenderContext: s.CreateCaptchaRenderContext(db.PortalRegisterSitekey),
	}, registerTemplate, nil
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

	data := &registerRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		CaptchaRenderContext: s.CreateCaptchaRenderContext(db.PortalRegisterSitekey),
	}

	ownerSource := &portalPropertyOwnerSource{Store: s.Store, Sitekey: data.CaptchaSitekey}

	captchaSolution := r.FormValue(captchaSolutionField)
	_, verr, err := s.PuzzleEngine.Verify(ctx, captchaSolution, ownerSource, time.Now().UTC())
	if err != nil || verr != puzzle.VerifyNoError {
		slog.ErrorContext(ctx, "Failed to verify captcha", "code", verr, common.ErrAttr(err))
		data.CaptchaError = "Captcha verification failed."
		s.render(w, r, registerFormTemplate, data)
		return
	}

	name := strings.TrimSpace(r.FormValue(common.ParamName))
	if len(name) < 3 {
		data.NameError = "Please use a longer name."
		s.render(w, r, registerFormTemplate, data)
		return
	}

	email := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if err := checkmail.ValidateFormat(email); err != nil {
		slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
		data.EmailError = "Email address is not valid."
		s.render(w, r, registerFormTemplate, data)
		return
	}

	if _, err := s.Store.Impl().FindUserByEmail(ctx, email); err == nil {
		slog.WarnContext(ctx, "User with such email already exists", "email", email)
		data.EmailError = "Such email is already registered. Login instead?"
		s.render(w, r, registerFormTemplate, data)
		return
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	sess := s.Sessions.SessionStart(w, r)
	_ = sess.Set(session.KeyLoginStep, loginStepSignUpVerify)
	_ = sess.Set(session.KeyUserEmail, email)
	_ = sess.Set(session.KeyUserName, name)
	_ = sess.Set(session.KeyTwoFactorCode, code)

	common.Redirect(s.RelURL(common.TwoFactorEndpoint), http.StatusOK, w, r)
}

func createInternalTrial(plan billing.Plan, status string) *dbgen.CreateSubscriptionParams {
	priceIDMonthly, _ := plan.PriceIDs()
	return &dbgen.CreateSubscriptionParams{
		ExternalProductID:      plan.ProductID(),
		ExternalPriceID:        priceIDMonthly,
		ExternalSubscriptionID: pgtype.Text{},
		ExternalCustomerID:     pgtype.Text{},
		Status:                 status,
		Source:                 dbgen.SubscriptionSourceInternal,
		TrialEndsAt:            db.Timestampz(time.Now().AddDate(0, 0, plan.TrialDays())),
		NextBilledAt:           db.Timestampz(time.Time{}),
	}
}

func (s *Server) doRegister(ctx context.Context, sess *common.Session) (*dbgen.User, *dbgen.Organization, error) {
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		return nil, nil, errIncompleteSession
	}

	name, ok := sess.Get(session.KeyUserName).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get user name from session")
		return nil, nil, errIncompleteSession
	}

	plan := s.PlanService.GetInternalTrialPlan()
	subscrParams := createInternalTrial(plan, s.PlanService.TrialStatus())

	var user *dbgen.User
	var org *dbgen.Organization

	if err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) error {
		var err error
		user, org, err = impl.CreateNewAccount(ctx, subscrParams, email, name, common.DefaultOrgName, -1 /*existing user ID*/)
		return err
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to create user account in Store", common.ErrAttr(err))
		return nil, nil, err
	}

	go common.RunOneOffJob(common.CopyTraceID(ctx, context.Background()), s.Jobs.OnboardUser(user))

	return user, org, nil
}

func (s *Server) OnboardUser(user *dbgen.User) common.OneOffJob {
	return &onboardUserJob{user: user, mailer: s.Mailer}
}

type onboardUserJob struct {
	user   *dbgen.User
	mailer common.Mailer
}

func (j *onboardUserJob) Name() string {
	return "OnboardUser"
}

func (j *onboardUserJob) InitialPause() time.Duration {
	return 0
}

func (j *onboardUserJob) RunOnce(ctx context.Context) error {
	return j.mailer.SendWelcome(ctx, j.user.Email)
}
