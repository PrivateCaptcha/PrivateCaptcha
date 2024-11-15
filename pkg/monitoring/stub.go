package monitoring

import "net/http"

type stubMetrics struct{}

func NewStub() *stubMetrics {
	return &stubMetrics{}
}

var _ Metrics = (*stubMetrics)(nil)

func (sm *stubMetrics) Handler(h http.Handler) http.Handler {
	return h
}
func (sm *stubMetrics) HandlerFunc(handlerIDFunc func() string) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return h
	}
}
