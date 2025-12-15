package monitoring

import (
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type stubMetrics struct{}

func NewStub() *stubMetrics {
	return &stubMetrics{}
}

var _ common.PlatformMetrics = (*Service)(nil)
var _ common.APIMetrics = (*Service)(nil)
var _ common.PortalMetrics = (*Service)(nil)

func (sm *stubMetrics) Handler(h http.Handler) http.Handler {
	return h
}
func (sm *stubMetrics) HandlerIDFunc(func() string) func(http.Handler) http.Handler {
	return common.NoopMiddleware
}

func (sm *stubMetrics) ObservePuzzleCreated(userID int32) {}

func (sm *stubMetrics) ObservePuzzleVerified(userID int32, result string, isStub bool) {}

func (sm *stubMetrics) ObserveHealth(postgres, clickhouse bool) {}
func (sm *stubMetrics) ObserveCacheHitRatio(ratio float64)      {}

func (sm *stubMetrics) ObserveHttpError(handlerID string, method string, code int) {}
func (sm *stubMetrics) ObserveApiError(handlerID string, method string, code int)  {}
