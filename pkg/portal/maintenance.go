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

	slog.DebugContext(ctx, "Updating Paddle prices", "interval", interval)

	ticker := time.NewTicker(interval)
	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
			ticker.Stop()
		case <-ticker.C:
			products := billing.GetProductsForStage(s.Stage)
			if prices, err := s.PaddleAPI.GetPrices(ctx, products); err == nil {
				if err = s.Store.CachePaddlePrices(ctx, prices); err != nil {
					slog.ErrorContext(ctx, "Failed to cache paddle prices", common.ErrAttr(err))
				}
			}
		}
	}

	slog.DebugContext(ctx, "Finished updating Paddle prices")
}

func (s *Server) gcSessions(ctx context.Context, interval time.Duration) {
	slog.DebugContext(ctx, "Clearing user sessions", "interval", interval)

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
