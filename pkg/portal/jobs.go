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

func (s *Server) OffboardUser(user *dbgen.User) common.OneOffJob {
	return &common.StubOneOffJob{}
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
