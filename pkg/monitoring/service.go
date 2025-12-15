package monitoring

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
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

const (
	MetricsNamespaceServer   = "server"
	MetricsNamespaceAPI      = "api"
	MetricsNamespaceCDN      = "cdn"
	MetricsNamespacePortal   = "portal"
	puzzleMetricsSubsystem   = "puzzle"
	platformMetricsSubsystem = "platform"
	userIDLabel              = "user_id"
	stubLabel                = "stub"
	resultLabel              = "result"
	// below is copy from go-http-metrics prometheus.go since they are not exposed publicly
	statusCodeLabel = "code"
	methodLabel     = "label"
	handlerIDLabel  = "handler"
	serviceLabel    = "service"
)

type Service struct {
	Registry               *prometheus.Registry
	fineAPIMiddleware      middleware.Middleware
	finePortalMiddleware   middleware.Middleware
	coarseServerMiddleware middleware.Middleware
	coarseCDNMiddleware    middleware.Middleware
	portalErrorCounter     *prometheus.CounterVec
	apiErrorCounter        *prometheus.CounterVec
	puzzleCounter          *prometheus.CounterVec
	verifyCounter          *prometheus.CounterVec
	hitRatioGauge          *prometheus.GaugeVec
	clickhouseHealthGauge  *prometheus.GaugeVec
	postgresHealthGauge    *prometheus.GaugeVec
}

var _ common.PlatformMetrics = (*Service)(nil)
var _ common.APIMetrics = (*Service)(nil)
var _ common.PortalMetrics = (*Service)(nil)

func traceID() string {
	return xid.New().String()
}

func Logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		ctx, _ := common.TraceContextFunc(r.Context(), traceID)

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
		ctx, tid := common.TraceContextFunc(r.Context(), traceID)
		headers := w.Header()
		headers[common.HeaderTraceID] = []string{tid}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func NewService() *Service {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	puzzleCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespaceAPI,
			Subsystem: puzzleMetricsSubsystem,
			Name:      "create_total",
			Help:      "Total number of puzzles created",
		},
		[]string{userIDLabel},
	)
	reg.MustRegister(puzzleCounter)

	verifyCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespaceAPI,
			Subsystem: puzzleMetricsSubsystem,
			Name:      "verify_total",
			Help:      "Total number of puzzle verifications",
		},
		[]string{stubLabel, userIDLabel, resultLabel},
	)
	reg.MustRegister(verifyCounter)

	portalErrorCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "fine", // this is the same as fine http metrics below to match go-http-metrics logic
			Subsystem: "http",
			Name:      "error_total",
			Help:      "Total number of Portal HTTP errors",
		},
		[]string{handlerIDLabel, statusCodeLabel, methodLabel, serviceLabel},
	)
	reg.MustRegister(portalErrorCounter)

	apiErrorCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "fine", // this is the same as fine http metrics below to match go-http-metrics logic
			Subsystem: "http",
			Name:      "error_total",
			Help:      "Total number of API specific errors",
		},
		[]string{handlerIDLabel, statusCodeLabel, methodLabel, serviceLabel},
	)
	reg.MustRegister(apiErrorCounter)

	clickhouseHealthGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespacePortal,
			Subsystem: platformMetricsSubsystem,
			Name:      "health_clickhouse",
			Help:      "Health status of ClickHouse",
		},
		[]string{},
	)
	reg.MustRegister(clickhouseHealthGauge)

	postgresHealthGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespaceServer,
			Subsystem: platformMetricsSubsystem,
			Name:      "health_postgres",
			Help:      "Health status of Postgres",
		},
		[]string{},
	)
	reg.MustRegister(postgresHealthGauge)

	hitRatioGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespaceServer,
			Subsystem: platformMetricsSubsystem,
			Name:      "cache_hit_ratio",
			Help:      "In-memory cache hit ratio",
		},
		[]string{},
	)
	reg.MustRegister(hitRatioGauge)

	fineRecorder := prometheus_metrics.NewRecorder(prometheus_metrics.Config{
		Prefix:          "fine",
		Registry:        reg,
		DurationBuckets: []float64{.05, .1, .25, .5, 1, 2.5},
	})

	coarseRecorder := prometheus_metrics.NewRecorder(prometheus_metrics.Config{
		Prefix:          "coarse",
		Registry:        reg,
		DurationBuckets: []float64{.05, .1, .5, 1, 2.5},
	})

	return &Service{
		Registry: reg,
		fineAPIMiddleware: middleware.New(middleware.Config{
			// this is added as Service label
			Service:            MetricsNamespaceAPI,
			DisableMeasureSize: true,
			Recorder:           fineRecorder,
		}),
		finePortalMiddleware: middleware.New(middleware.Config{
			// this is added as Service label
			Service:            MetricsNamespacePortal,
			DisableMeasureSize: true,
			Recorder:           fineRecorder,
		}),
		coarseServerMiddleware: middleware.New(middleware.Config{
			// this is added as Service label
			Service:                MetricsNamespaceServer,
			GroupedStatus:          true,
			DisableMeasureSize:     true,
			DisableMeasureInflight: true,
			Recorder:               coarseRecorder,
		}),
		coarseCDNMiddleware: middleware.New(middleware.Config{
			// this is added as Service label
			Service:                MetricsNamespaceCDN,
			GroupedStatus:          true,
			DisableMeasureSize:     true,
			DisableMeasureInflight: true,
			Recorder:               coarseRecorder,
		}),
		puzzleCounter:         puzzleCounter,
		verifyCounter:         verifyCounter,
		hitRatioGauge:         hitRatioGauge,
		clickhouseHealthGauge: clickhouseHealthGauge,
		postgresHealthGauge:   postgresHealthGauge,
		portalErrorCounter:    portalErrorCounter,
		apiErrorCounter:       apiErrorCounter,
	}
}

// this belongs only to APIMetrics interface (at this time)
func (s *Service) Handler(h http.Handler) http.Handler {
	// handlerID is taken from the request path in this case
	return std.Handler("", s.fineAPIMiddleware, h)
}

func (s *Service) CDNHandler(h http.Handler) http.Handler {
	// handlerID is taken from the request path in this case
	return std.Handler("", s.coarseCDNMiddleware, h)
}

func (s *Service) IgnoredHandler(h http.Handler) http.Handler {
	return std.Handler("_ignored", s.coarseServerMiddleware, h)
}

func (s *Service) HandlerIDFunc(handlerIDFunc func() string) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		handlerID := handlerIDFunc()
		return std.Handler(handlerID, s.finePortalMiddleware, h)
	}
}

func (s *Service) ObserveApiError(handlerID string, method string, code int) {
	s.apiErrorCounter.With(prometheus.Labels{
		handlerIDLabel:  handlerID,
		statusCodeLabel: strconv.Itoa(code),
		methodLabel:     method,
		serviceLabel:    MetricsNamespaceAPI,
	}).Inc()
}

func (s *Service) ObserveHttpError(handlerID string, method string, code int) {
	s.portalErrorCounter.With(prometheus.Labels{
		handlerIDLabel:  handlerID,
		statusCodeLabel: strconv.Itoa(code),
		methodLabel:     method,
		serviceLabel:    MetricsNamespacePortal,
	}).Inc()
}

func (s *Service) ObservePuzzleCreated(userID int32) {
	s.puzzleCounter.With(prometheus.Labels{
		userIDLabel: strconv.Itoa(int(userID)),
	}).Inc()
}

func (s *Service) ObserveCacheHitRatio(ratio float64) {
	s.hitRatioGauge.With(prometheus.Labels{}).Set(ratio)
}

func (s *Service) ObservePuzzleVerified(userID int32, result string, isStub bool) {
	s.verifyCounter.With(prometheus.Labels{
		stubLabel:   strconv.FormatBool(isStub),
		resultLabel: result,
		userIDLabel: strconv.Itoa(int(userID)),
	}).Inc()
}

func (s *Service) ObserveHealth(postgres, clickhouse bool) {
	var chVal, pgVal float64

	if postgres {
		pgVal = 1
	} else {
		pgVal = 0
	}

	if clickhouse {
		chVal = 1
	} else {
		chVal = 0
	}

	s.postgresHealthGauge.With(prometheus.Labels{}).Set(pgVal)
	s.clickhouseHealthGauge.With(prometheus.Labels{}).Set(chVal)
}

func (s *Service) Setup(mux *http.ServeMux) {
	mux.Handle(http.MethodGet+" /metrics", common.Recovered(promhttp.HandlerFor(s.Registry, promhttp.HandlerOpts{Registry: s.Registry})))
	s.setupProfiling(context.TODO(), mux)
}
