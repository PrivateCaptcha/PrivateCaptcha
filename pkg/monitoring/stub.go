package monitoring

import (
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type stubMetrics struct{}

func NewStub() *stubMetrics {
	return &stubMetrics{}
}

var _ common.Metrics = (*stubMetrics)(nil)

func (sm *stubMetrics) Handler(h http.Handler) http.Handler {
	return h
}
func (sm *stubMetrics) HandlerFunc(func() string) func(http.Handler) http.Handler {
	return common.NoopMiddleware
}

func (sm *stubMetrics) ObservePuzzleCreated(userID int32) {}

func (sm *stubMetrics) ObservePuzzleVerified(userID int32, result string, isStub bool) {}

func (sm *stubMetrics) ObserveHealth(postgres, clickhouse bool) {}
