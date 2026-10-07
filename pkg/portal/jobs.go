package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

const (
	spammerEmail = "spammer@privatecaptcha.local"
)

func (s *Server) OnboardUser(user *dbgen.User, plan billing.Plan) common.OneOffJob {
	return &onboardUserJob{user: user, mailer: s.Mailer, store: s.Store}
}

func (s *Server) OffboardUser(user *dbgen.User, subscription *dbgen.Subscription) common.OneOffJob {
	return &OffboardUserJob{
		user:         user,
		subscription: subscription,
		planService:  s.PlanService,
	}
}

// runOffboardJob launches the offboard (external subscription cancellation)
// job on a goroutine that is tracked by jobsWG and runs against shutdownCtx.
// Tracking lets Shutdown wait for the cancellation to complete during a
// deploy/SIGTERM (the in-flight request is gone), and shutdownCtx (derived
// from context.Background(), not the request) ensures the job is not cancelled
// when the HTTP response is sent. This replaces the previous detached,
// untracked `go common.RunOneOffJob(... context.Background() ...)` that could
// be silently dropped by a deploy or crash between the soft-delete commit and
// CancelSubscription returning.
func (s *Server) runOffboardJob(ctx context.Context, user *dbgen.User, subscription *dbgen.Subscription) {
	job := s.Jobs.OffboardUser(user, subscription)
	shutdownCtx := s.shutdownCtx
	if shutdownCtx == nil {
		shutdownCtx = context.Background()
	}
	s.jobsWG.Add(1)
	go func() {
		defer s.jobsWG.Done()
		common.RunOneOffJob(common.CopyTraceID(ctx, shutdownCtx), job, job.NewParams())
	}()
}

func (s *Server) CheckRegistration(sess *session.Session, r *http.Request, orgInviteID int32) common.OneOffJob {
	return &registrationCheckJob{
		Sess:         sess,
		Store:        s.Store,
		SessionStore: s.Sessions.Store,
		Email:        strings.TrimSpace(r.FormValue(common.ParamEmail)),
		OrgInviteID:  orgInviteID,
	}
}

func (s *Server) LoginUser(sess *session.Session) common.OneOffJob {
	return &LoginUserJob{
		Sess:  sess,
		Store: s.Store,
	}
}

type onboardUserJob struct {
	user   *dbgen.User
	mailer common.Mailer
	store  db.Implementor
}

func (j *onboardUserJob) Name() string {
	return "OnboardUser"
}

func (j *onboardUserJob) InitialPause() time.Duration {
	return 0
}

func (j *onboardUserJob) NewParams() any {
	return struct{}{}
}

func (j *onboardUserJob) RunOnce(ctx context.Context, params any) error {
	return j.mailer.SendWelcome(ctx, j.user.Email, common.GuessFirstName(j.user.Name, j.user.Email))
}

type OffboardUserJob struct {
	user         *dbgen.User
	subscription *dbgen.Subscription
	planService  billing.PlanService
}

func (j *OffboardUserJob) Name() string {
	return "OffboardUser"
}

func (j *OffboardUserJob) InitialPause() time.Duration {
	return 0
}

func (j *OffboardUserJob) NewParams() any {
	return struct{}{}
}

func (j *OffboardUserJob) RunOnce(ctx context.Context, params any) error {
	if j.subscription == nil || !j.planService.IsSubscriptionActive(j.subscription.Status) ||
		!j.subscription.ExternalSubscriptionID.Valid || HasScheduledCancellation(j.subscription) {
		return nil
	}

	if err := j.planService.CancelSubscription(ctx, j.subscription.ExternalSubscriptionID.String); err != nil {
		slog.ErrorContext(ctx, "Failed to cancel external subscription", "userID", j.user.ID, common.ErrAttr(err))
		return err
	}

	return nil
}

type LoginUserJob struct {
	Sess  *session.Session
	Store db.Implementor
}

type registrationCheckJob struct {
	Sess         *session.Session
	Store        db.Implementor
	SessionStore session.Store
	Email        string
	OrgInviteID  int32
}

func (j *registrationCheckJob) Name() string {
	return "RegistrationCheck"
}
func (j *registrationCheckJob) InitialPause() time.Duration {
	return 0
}
func (j *registrationCheckJob) NewParams() any {
	return struct{}{}
}
func (j *registrationCheckJob) RunOnce(ctx context.Context, params any) error {
	if j.Sess == nil {
		return nil
	}
	if j.Store != nil && j.OrgInviteID > 0 {
		_, _ = j.Store.Impl().RetrieveOrgInviteByID(ctx, j.OrgInviteID)
	}

	if strings.EqualFold(j.Email, spammerEmail) {
		slog.WarnContext(ctx, "Requiring verification for registration", "reason", "email", common.SessionHashAttr(j.Sess.Hash()))
		return j.SessionStore.SetVerifyRegistration(ctx, j.Sess.ID(), true)
	}

	return nil
}

func (j *LoginUserJob) Name() string {
	return "LoginUser"
}
func (j *LoginUserJob) InitialPause() time.Duration {
	return 0
}
func (j *LoginUserJob) NewParams() any {
	return struct{}{}
}
func (j *LoginUserJob) RunOnce(ctx context.Context, params any) error {
	authority, ok := j.Sess.Authority()
	if ok && authority.State == session.StateAuthenticated && authority.UserID > 0 {
		j.Store.AuditLog().RecordEvent(ctx, newUserAuthAuditLogEvent(authority.UserID, common.AuditLogActionLogin), common.AuditLogSourcePortal)

		slog.DebugContext(ctx, "Fetching system notification for user", "userID", authority.UserID)
		if n, err := j.Store.Impl().RetrieveSystemUserNotification(ctx, time.Now().UTC(), authority.UserID); err == nil {
			if serr := j.Sess.Set(ctx, session.KeyNotificationID, n.ID); serr != nil {
				slog.WarnContext(ctx, "Failed to set session value", common.ErrAttr(serr))
			}
		}
	} else {
		slog.ErrorContext(ctx, "Authenticated Authority not found in session")
	}

	return nil
}
