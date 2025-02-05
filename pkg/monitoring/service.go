package monitoring

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/xid"
	prometheus_metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	"github.com/slok/go-http-metrics/middleware/std"
)

const (
	metricsNamespace = "server"
	metricsSubsystem = "puzzle"
	userIDLabel      = "user_id"
	stubLabel        = "stub"
	resultLabel      = "result"
)

type Metrics interface {
	Handler(h http.Handler) http.Handler
	HandlerFunc(handlerIDFunc func() string) func(http.Handler) http.Handler
	ObservePuzzleCreated(userID int32, isStub bool)
	ObservePuzzleVerified(userID int32, result puzzle.VerifyError, isStub bool)
}

type service struct {
	registry    *prometheus.Registry
	middleware  middleware.Middleware
	puzzleCount *prometheus.CounterVec
	verifyCount *prometheus.CounterVec
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

	puzzleCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "create_total",
			Help:      "Total number of puzzles created",
		},
		[]string{stubLabel, userIDLabel},
	)
	reg.MustRegister(puzzleCount)

	verifyCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "verify_total",
			Help:      "Total number of puzzle verifications",
		},
		[]string{stubLabel, userIDLabel, resultLabel},
	)
	reg.MustRegister(verifyCount)

	return &service{
		registry: reg,
		middleware: middleware.New(middleware.Config{
			Service: metricsNamespace,
			Recorder: prometheus_metrics.NewRecorder(prometheus_metrics.Config{
				// this is added as Service label
				// Prefix:   metricsNamespace,
				Registry: reg,
			}),
		}),
		puzzleCount: puzzleCount,
		verifyCount: verifyCount,
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

func (s *service) ObservePuzzleCreated(userID int32, isStub bool) {
	s.puzzleCount.With(prometheus.Labels{
		userIDLabel: strconv.Itoa(int(userID)),
		stubLabel:   strconv.FormatBool(isStub),
	}).Inc()
}

func (s *service) ObservePuzzleVerified(userID int32, result puzzle.VerifyError, isStub bool) {
	s.verifyCount.With(prometheus.Labels{
		stubLabel:   strconv.FormatBool(isStub),
		resultLabel: result.String(),
		userIDLabel: strconv.Itoa(int(userID)),
	}).Inc()
}

func (s *service) Setup(mux *http.ServeMux) {
	mux.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{Registry: s.registry}))
	s.setupProfiling(context.TODO(), mux)
}
