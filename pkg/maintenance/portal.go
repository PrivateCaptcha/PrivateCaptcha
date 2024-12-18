package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	paddlePricesAttempts = 5
)

type PaddlePricesJob struct {
	Stage     string
	PaddleAPI billing.PaddleAPI
	Store     *db.BusinessStore
}

var _ common.PeriodicJob = (*PaddlePricesJob)(nil)

func (j *PaddlePricesJob) Interval() time.Duration {
	return 30 * time.Minute
}

func (j *PaddlePricesJob) Jitter() time.Duration {
	return 5 * time.Minute
}

func (j *PaddlePricesJob) Name() string {
	return "paddle_prices_job"
}

func (j *PaddlePricesJob) RunOnce(ctx context.Context) error {
	products := billing.GetProductsForStage(j.Stage)
	prices, err := j.PaddleAPI.GetPrices(ctx, products)
	if err == nil {
		if err = j.Store.CachePaddlePrices(ctx, prices); err != nil {
			slog.ErrorContext(ctx, "Failed to cache paddle prices", common.ErrAttr(err))
		}

		billing.UpdatePlansPrices(prices, j.Stage)
	}

	return err
}

type SessionsCleanupJob struct {
	Session session.Manager
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

type WarmupPaddlePrices struct {
	Store *db.BusinessStore
	Stage string
}

var _ common.OneOffJob = (*WarmupPaddlePrices)(nil)

func (j *WarmupPaddlePrices) Name() string {
	return "warmup_paddle_prices"
}

func (j *WarmupPaddlePrices) InitialPause() time.Duration {
	return 5 * time.Second
}

func (j *WarmupPaddlePrices) RunOnce(ctx context.Context) error {
	prices, err := j.Store.RetrievePaddlePrices(ctx)
	if err == nil {
		billing.UpdatePlansPrices(prices, j.Stage)
	} else {
		slog.WarnContext(ctx, "Paddle prices are not cached properly", common.ErrAttr(err))
	}

	return nil
}

type WarmupPortalAuth struct {
	Store *db.BusinessStore
}

var _ common.OneOffJob = (*WarmupPortalAuth)(nil)

func (j *WarmupPortalAuth) Name() string {
	return "warmup_portal_auth"
}

func (j *WarmupPortalAuth) InitialPause() time.Duration {
	return 5 * time.Second
}

func (j *WarmupPortalAuth) RunOnce(ctx context.Context) error {
	portalUUID := pgtype.UUID{}
	if err := portalUUID.Scan(db.PortalPropertyID); err != nil {
		return err
	}
	sitekey := db.UUIDToSiteKey(portalUUID)

	if _, err := j.Store.RetrievePropertiesBySitekey(ctx, map[string]struct{}{sitekey: {}}); err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties by sitekey", common.ErrAttr(err))
	}

	return nil
}
