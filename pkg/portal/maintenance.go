package portal

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func (s *Server) updatePaddlePrices(ctx context.Context, interval time.Duration, initialPause time.Duration) {
	time.Sleep(initialPause)

	if prices, err := s.Store.RetrievePaddlePrices(ctx); err == nil {
		billing.UpdatePlansPrices(prices, s.Stage)
	} else {
		slog.WarnContext(ctx, "Paddle prices are not cached properly", common.ErrAttr(err))
	}

	slog.DebugContext(ctx, "Updating Paddle prices", "interval", interval.String())
	const paddlePricesLock = "paddle_prices_job"

	ticker := time.NewTicker(interval)
	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
			ticker.Stop()
		case <-ticker.C:
			tnow := time.Now().UTC()
			// NOTE: same logic as in api/maintenance, where we do not intend to release the lock in normal circumstances
			if _, err := s.Store.AcquireLock(ctx, paddlePricesLock, nil /*data*/, tnow.Add(interval-1*time.Second)); err == nil {
				products := billing.GetProductsForStage(s.Stage)
				if prices, err := s.PaddleAPI.GetPrices(ctx, products); err == nil {
					if err = s.Store.CachePaddlePrices(ctx, prices); err != nil {
						slog.ErrorContext(ctx, "Failed to cache paddle prices", common.ErrAttr(err))
					}

					billing.UpdatePlansPrices(prices, s.Stage)
				} else {
					s.Store.ReleaseLock(ctx, paddlePricesLock)
				}
			}
		}
	}

	slog.DebugContext(ctx, "Finished updating Paddle prices")
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
