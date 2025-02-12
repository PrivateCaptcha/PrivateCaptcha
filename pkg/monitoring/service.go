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
	ObservePuzzleCreated(userID int32)
	ObservePuzzleVerified(userID int32, result puzzle.VerifyError, isStub bool)
}

type service struct {
	registry        *prometheus.Registry
	stdMiddleware   middleware.Middleware
	roughMiddleware middleware.Middleware
	puzzleCount     *prometheus.CounterVec
	verifyCount     *prometheus.CounterVec
}

var _ Metrics = (*service)(nil)

func traceID() string {
	return xid.New().String()
}

func Logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		ctx := common.TraceContextFunc(r.Context(), traceID)

		// NOTE: these data (path, method, time) are now available as prometheus metrics
		slog.Log(ctx, common.LevelTrace, "Started request", "path", r.URL.Path, "method", r.Method)
		defer func() {
			slog.Log(ctx, common.LevelTrace, "Finished request", "path", r.URL.Path, "method", r.Method,
				"duration", time.Since(t).Milliseconds())
		}()

		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func Traced(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := common.TraceContextFunc(r.Context(), traceID)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func NewService() *service {
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
		[]string{userIDLabel},
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
		stdMiddleware: middleware.New(middleware.Config{
			Service:            metricsNamespace,
			DisableMeasureSize: true,
			Recorder: prometheus_metrics.NewRecorder(prometheus_metrics.Config{
				// this is added as Service label
				// Prefix:   metricsNamespace,
				Registry:        reg,
				DurationBuckets: []float64{0.2, 0.4, 0.8, 1.6, 3.2},
			}),
		}),
		roughMiddleware: middleware.New(middleware.Config{
			Service:                metricsNamespace,
			GroupedStatus:          true,
			DisableMeasureSize:     true,
			DisableMeasureInflight: true,
			Recorder: prometheus_metrics.NewRecorder(prometheus_metrics.Config{
				// this is added as Service label
				// Prefix:   metricsNamespace,
				Registry:        reg,
				DurationBuckets: []float64{0.01, 0.16, 0.64, 1.28},
			}),
		}),
		puzzleCount: puzzleCount,
		verifyCount: verifyCount,
	}
}

func (s *service) Handler(h http.Handler) http.Handler {
	// handlerID is taken from the request path in this case
	return std.Handler("", s.stdMiddleware, h)
}

func (s *service) CDNHandler(h http.Handler) http.Handler {
	// handlerID is taken from the request path in this case
	return std.Handler("", s.roughMiddleware, h)
}

func (s *service) IgnoredHandler(h http.Handler) http.Handler {
	return std.Handler("_ignored", s.roughMiddleware, h)
}

func (s *service) HandlerFunc(handlerIDFunc func() string) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		handlerID := handlerIDFunc()
		return std.Handler(handlerID, s.stdMiddleware, h)
	}
}

func (s *service) ObservePuzzleCreated(userID int32) {
	s.puzzleCount.With(prometheus.Labels{
		userIDLabel: strconv.Itoa(int(userID)),
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
