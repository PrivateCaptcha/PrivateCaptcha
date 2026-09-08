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

func (s *Server) OnboardUser(user *dbgen.User, plan billing.Plan, orgInviteID *int32) common.OneOffJob {
	return &onboardUserJob{user: user, mailer: s.Mailer, store: s.Store, orgInviteID: orgInviteID}
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
	userName := common.GuessFirstName(j.user.Name, j.user.Email)
	err := j.mailer.SendWelcome(ctx, j.user.Email, userName)

	if j.orgInviteID != nil && *j.orgInviteID > 0 {
		orgUser, inviteErr := j.store.Impl().GetCachedOrgInviteByID(ctx, *j.orgInviteID)
		if inviteErr != nil {
			slog.ErrorContext(ctx, "Failed to retrieve org invite", "inviteID", *j.orgInviteID, "userID", j.user.ID, common.ErrAttr(inviteErr))
		} else if !orgUser.UserID.Valid || orgUser.UserID.Int32 != j.user.ID {
			slog.DebugContext(ctx, "Org invite is not linked to onboarded user", "inviteID", *j.orgInviteID, "userID", j.user.ID)
		} else if org, _, err := j.store.Impl().RetrieveUserOrganization(ctx, j.user, orgUser.OrgID); err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve organization for linked invite", "inviteID", *j.orgInviteID, "userID", j.user.ID, common.ErrAttr(err))
		} else if owner, err := j.store.Impl().RetrieveUser(ctx, org.UserID.Int32); err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve organization owner for linked invite", "orgID", org.ID, "userID", j.user.ID, common.ErrAttr(err))
		} else if err := j.mailer.SendOrgMemberJoined(ctx, owner.Email, common.GuessFirstName(owner.Name, owner.Email),
			userName, j.user.Email, org.Name); err != nil {
			slog.ErrorContext(ctx, "Failed to send organization member joined email", "orgID", org.ID, "userID", j.user.ID, common.ErrAttr(err))
		}
	}

	return err
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
		return j.SessionStore.SetVerifyRegistration(ctx, j.Sess.ID())
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
