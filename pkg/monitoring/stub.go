package monitoring

import (
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type stubMetrics struct{}

func NewStub() *stubMetrics {
	return &stubMetrics{}
}

var _ common.PlatformMetrics = (*stubMetrics)(nil)
var _ common.APIMetrics = (*stubMetrics)(nil)
var _ common.PortalMetrics = (*stubMetrics)(nil)
var _ common.BaseMetrics = (*stubMetrics)(nil)

func (sm *stubMetrics) PortalHandler(h http.Handler) http.Handler { return h }
func (sm *stubMetrics) APIHandler(h http.Handler) http.Handler    { return h }
func (sm *stubMetrics) PortalHandlerIDFunc(func() string) func(http.Handler) http.Handler {
	return common.NoopMiddleware
}
func (sm *stubMetrics) APIHandlerIDFunc(func() string) func(http.Handler) http.Handler {
	return common.NoopMiddleware
}

func (sm *stubMetrics) ObservePuzzleCreated(userID int32) {}

func (sm *stubMetrics) ObservePuzzleVerified(userID int32, result string, isStub bool) {}

func (sm *stubMetrics) ObserveHealth(postgres, clickhouse bool) {}
func (sm *stubMetrics) ObserveCacheHitRatio(ratio float64)      {}
func (sm *stubMetrics) ObserveQueryDuration(duration float64)   {}
func (sm *stubMetrics) ObserveQueryError()                      {}

func (sm *stubMetrics) ObserveHttpError(handlerID string, method string, code int) {}
func (sm *stubMetrics) ObserveApiError(handlerID string, method string, code int)  {}

func (sm *stubMetrics) ObserveEventDropped(eventType common.MetricEventType) {}
func (sm *stubMetrics) ObservePanic()                                        {}
