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
	MetricsNamespace       = "server"
	puzzleMetricsSubsystem = "puzzle"
	userIDLabel            = "user_id"
	stubLabel              = "stub"
	resultLabel            = "result"
)

type Metrics interface {
	Handler(h http.Handler) http.Handler
	HandlerFunc(handlerIDFunc func() string) func(http.Handler) http.Handler
	ObservePuzzleCreated(userID int32)
	ObservePuzzleVerified(userID int32, result puzzle.VerifyError, isStub bool)
}

type Service struct {
	Registry         *prometheus.Registry
	fineMiddleware   middleware.Middleware
	coarseMiddleware middleware.Middleware
	puzzleCount      *prometheus.CounterVec
	verifyCount      *prometheus.CounterVec
}

var _ Metrics = (*Service)(nil)

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

func NewService() *Service {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	puzzleCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: puzzleMetricsSubsystem,
			Name:      "create_total",
			Help:      "Total number of puzzles created",
		},
		[]string{userIDLabel},
	)
	reg.MustRegister(puzzleCount)

	verifyCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: puzzleMetricsSubsystem,
			Name:      "verify_total",
			Help:      "Total number of puzzle verifications",
		},
		[]string{stubLabel, userIDLabel, resultLabel},
	)
	reg.MustRegister(verifyCount)

	return &Service{
		Registry: reg,
		fineMiddleware: middleware.New(middleware.Config{
			// this is added as Service label
			Service:            MetricsNamespace,
			DisableMeasureSize: true,
			Recorder: prometheus_metrics.NewRecorder(prometheus_metrics.Config{
				Prefix:          "fine",
				Registry:        reg,
				DurationBuckets: []float64{.05, .1, .25, .5, 1, 2.5},
			}),
		}),
		coarseMiddleware: middleware.New(middleware.Config{
			// this is added as Service label
			Service:                MetricsNamespace,
			GroupedStatus:          true,
			DisableMeasureSize:     true,
			DisableMeasureInflight: true,
			Recorder: prometheus_metrics.NewRecorder(prometheus_metrics.Config{
				Prefix:          "coarse",
				Registry:        reg,
				DurationBuckets: []float64{.05, .1, .5, 1, 2.5},
			}),
		}),
		puzzleCount: puzzleCount,
		verifyCount: verifyCount,
	}
}

func (s *Service) Handler(h http.Handler) http.Handler {
	// handlerID is taken from the request path in this case
	return std.Handler("", s.fineMiddleware, h)
}

func (s *Service) CDNHandler(h http.Handler) http.Handler {
	// handlerID is taken from the request path in this case
	return std.Handler("", s.coarseMiddleware, h)
}

func (s *Service) IgnoredHandler(h http.Handler) http.Handler {
	return std.Handler("_ignored", s.coarseMiddleware, h)
}

func (s *Service) HandlerFunc(handlerIDFunc func() string) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		handlerID := handlerIDFunc()
		return std.Handler(handlerID, s.fineMiddleware, h)
	}
}

func (s *Service) ObservePuzzleCreated(userID int32) {
	s.puzzleCount.With(prometheus.Labels{
		userIDLabel: strconv.Itoa(int(userID)),
	}).Inc()
}

func (s *Service) ObservePuzzleVerified(userID int32, result puzzle.VerifyError, isStub bool) {
	s.verifyCount.With(prometheus.Labels{
		stubLabel:   strconv.FormatBool(isStub),
		resultLabel: result.String(),
		userIDLabel: strconv.Itoa(int(userID)),
	}).Inc()
}

func (s *Service) Setup(mux *http.ServeMux) {
	mux.Handle("/metrics", promhttp.HandlerFor(s.Registry, promhttp.HandlerOpts{Registry: s.Registry}))
	s.setupProfiling(context.TODO(), mux)
}
