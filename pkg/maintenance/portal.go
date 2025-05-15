package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5/pgtype"
)

type SessionsCleanupJob struct {
	Session *session.Manager
}

var _ common.PeriodicJob = (*SessionsCleanupJob)(nil)

func (j *SessionsCleanupJob) Interval() time.Duration {
	return j.Session.MaxLifetime
}

func (j *SessionsCleanupJob) Jitter() time.Duration {
	return 1
}

func (j *SessionsCleanupJob) Name() string {
	return "sessions_cleanup_job"
}

func (j *SessionsCleanupJob) RunOnce(ctx context.Context) error {
	j.Session.GC(ctx)

	return nil
}

type WarmupPortalAuth struct {
	Store db.Implementor
}

var _ common.OneOffJob = (*WarmupPortalAuth)(nil)

func (j *WarmupPortalAuth) Name() string {
	return "warmup_portal_auth"
}

func (j *WarmupPortalAuth) InitialPause() time.Duration {
	return 5 * time.Second
}

func (j *WarmupPortalAuth) RunOnce(ctx context.Context) error {
	sitekeys := make(map[string]struct{})

	loginUUID := pgtype.UUID{}
	if err := loginUUID.Scan(db.PortalLoginPropertyID); err == nil {
		loginSitekey := db.UUIDToSiteKey(loginUUID)
		sitekeys[loginSitekey] = struct{}{}
	} else {
		return err
	}

	registerUUID := pgtype.UUID{}
	if err := registerUUID.Scan(db.PortalRegisterPropertyID); err == nil {
		registerSitekey := db.UUIDToSiteKey(registerUUID)
		sitekeys[registerSitekey] = struct{}{}
	} else {
		return err
	}

	if _, err := j.Store.Impl().RetrievePropertiesBySitekey(ctx, sitekeys); err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
	}

	return nil
}
