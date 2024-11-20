package monitoring

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/xid"
	prometheus_metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	"github.com/slok/go-http-metrics/middleware/std"
)

type Metrics interface {
	Handler(h http.Handler) http.Handler
	HandlerFunc(handlerIDFunc func() string) func(http.Handler) http.Handler
}

type service struct {
	address    string
	registry   *prometheus.Registry
	server     *http.Server
	middleware middleware.Middleware
}

var _ Metrics = (*service)(nil)

func traceID() string {
	return xid.New().String()
}

func Logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		ctx := common.TraceContextFunc(r.Context(), traceID)

		slog.DebugContext(ctx, "Started request", "path", r.URL.Path, "method", r.Method)
		defer func() {
			slog.DebugContext(ctx, "Finished request", "path", r.URL.Path, "method", r.Method,
				"duration", time.Since(t).Milliseconds())
		}()

		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func NewService(getenv func(string) string) *service {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return &service{
		registry: reg,
		address:  getenv("PC_METRICS_ADDRESS"),
		middleware: middleware.New(middleware.Config{
			Recorder: prometheus_metrics.NewRecorder(prometheus_metrics.Config{
				Registry: reg,
			}),
		}),
	}
}

func (s *service) Handler(h http.Handler) http.Handler {
	// handlerID is taken from the request path in this case
	return std.Handler("", s.middleware, h)
}

func (s *service) HandlerFunc(handlerIDFunc func() string) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		handlerID := handlerIDFunc()
		return std.Handler(handlerID, s.middleware, h)
	}
}

func (s *service) StartServing(ctx context.Context) {
	if len(s.address) == 0 {
		slog.WarnContext(ctx, "Metrics serving address is empty")
		return
	}

	metricsRouter := http.NewServeMux()
	metricsRouter.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{Registry: s.registry}))

	s.server = &http.Server{
		Addr:    s.address,
		Handler: metricsRouter,
	}

	go func() {
		slog.InfoContext(ctx, "Serving metrics", "address", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(ctx, "Error serving metrics", common.ErrAttr(err))
		}
	}()
}

func (s *service) Shutdown() {
	if s.server != nil {
		s.server.Close()
	}
}
