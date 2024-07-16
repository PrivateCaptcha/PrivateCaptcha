package portal

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type PaddlePricesJob struct {
	Stage     string
	PaddleAPI billing.PaddleAPI
	Store     *db.BusinessStore
}

var _ common.PeriodicJob = (*PaddlePricesJob)(nil)

func (j *PaddlePricesJob) Interval() time.Duration {
	return 6 * time.Hour
}

func (j *PaddlePricesJob) Jitter() time.Duration {
	return j.Interval() / 2
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

func (s *Server) warmupPaddlePrices(ctx context.Context, initialPause time.Duration) {
	time.Sleep(initialPause)

	if prices, err := s.Store.RetrievePaddlePrices(ctx); err == nil {
		billing.UpdatePlansPrices(prices, s.Stage)
	} else {
		slog.WarnContext(ctx, "Paddle prices are not cached properly", common.ErrAttr(err))
	}
}

func (s *Server) gcSessions(ctx context.Context, interval time.Duration) {
	slog.DebugContext(ctx, "Clearing user sessions", "interval", interval.String())

	ticker := time.NewTicker(interval)
	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
			ticker.Stop()
		case <-ticker.C:
			s.Session.GC(ctx)
		}
	}

	slog.DebugContext(ctx, "Finished clearing sessions")
}
