package portal

import (
	"context"
	"log/slog"
	"net/http"
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

type Jobs interface {
	OnboardUser(user *dbgen.User, plan billing.Plan, orgInviteID *int32) common.OneOffJob
	OffboardUser(user *dbgen.User) common.OneOffJob
	LoginUser(sess *session.Session) common.OneOffJob
	CheckRegistration(sess *session.Session, r *http.Request) common.OneOffJob
}

func (s *Server) OnboardUser(user *dbgen.User, plan billing.Plan, orgInviteID *int32) common.OneOffJob {
	return &onboardUserJob{user: user, mailer: s.Mailer, store: s.Store, orgInviteID: orgInviteID}
}

func (s *Server) OffboardUser(user *dbgen.User) common.OneOffJob {
	return &common.StubOneOffJob{}
}

func (s *Server) CheckRegistration(sess *session.Session, r *http.Request) common.OneOffJob {
	return &registrationCheckJob{Sess: sess}
}

func (s *Server) LoginUser(sess *session.Session) common.OneOffJob {
	return &LoginUserJob{
		Sess:  sess,
		Store: s.Store,
	}
}

type onboardUserJob struct {
	user        *dbgen.User
	mailer      common.Mailer
	store       db.Implementor
	orgInviteID *int32
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
	// Link org invite if present
	if j.orgInviteID != nil && *j.orgInviteID > 0 {
		if err := j.store.Impl().LinkOrgInviteToUser(ctx, *j.orgInviteID, j.user); err != nil {
			slog.ErrorContext(ctx, "Failed to link org invite to user", "inviteID", *j.orgInviteID, "userID", j.user.ID, common.ErrAttr(err))
			// Don't return error - this is a non-critical failure
		} else {
			slog.InfoContext(ctx, "Linked org invite to user", "inviteID", *j.orgInviteID, "userID", j.user.ID)
		}
	}

	return j.mailer.SendWelcome(ctx, j.user.Email, common.GuessFirstName(j.user.Name, j.user.Email))
}

type LoginUserJob struct {
	Sess  *session.Session
	Store db.Implementor
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
	userID, hasUserID := j.Sess.Get(ctx, session.KeyUserID).(int32)
	if hasUserID {
		j.Store.AuditLog().RecordEvent(ctx, newUserAuthAuditLogEvent(userID, common.AuditLogActionLogin), common.AuditLogSourcePortal)

		slog.DebugContext(ctx, "Fetching system notification for user", "userID", userID)
		if n, err := j.Store.Impl().RetrieveSystemUserNotification(ctx, time.Now().UTC(), userID); err == nil {
			if serr := j.Sess.Set(ctx, session.KeyNotificationID, n.ID); serr != nil {
				slog.WarnContext(ctx, "Failed to set session value", common.ErrAttr(serr))
			}
		}
	} else {
		slog.ErrorContext(ctx, "UserID not found in session")
	}

	return nil
}

type registrationCheckJob struct {
	Sess *session.Session
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

	email, hasEmail := j.Sess.Get(ctx, session.KeyUserEmail).(string)
	if !hasEmail {
		return nil
	}

	if email == spammerEmail {
		slog.WarnContext(ctx, "Requiring verification for registration", "reason", "email", common.SessionIDAttr(j.Sess.ID()))
		if serr := j.Sess.Set(ctx, session.KeyVerifyRegistration, true); serr != nil {
			slog.WarnContext(ctx, "Failed to set session value", common.ErrAttr(serr))
		}
	}

	return nil
}
