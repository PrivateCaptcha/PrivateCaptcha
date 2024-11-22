//go:build !profile

package monitoring

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func (s *service) setupProfiling(ctx context.Context, mux *http.ServeMux) {
	slog.Log(ctx, common.LevelTrace, "Profiling is not enabled during compile time")
}
