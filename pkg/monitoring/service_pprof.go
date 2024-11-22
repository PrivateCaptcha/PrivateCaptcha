//go:build profile

package monitoring

import (
	"log/slog"
	"net/http"
	"net/http/pprof"
)

// NOTE: (default) alternative would be to _ import the pprof package and start http server on :6060
func (s *service) setupProfiling(mux *http.ServeMux) {
	slog.DebugContext(ctx, "Enabling profiling endpoints")

	mux.HandleFunc("/debug/pprof/", pprof.Index)

	profiles := []string{"goroutine", "heap", "allocs", "threadcreate", "block", "mutex"}
	for _, p := range profiles {
		mux.Handle("/debug/pprof/"+p, pprof.Handler(p))
	}

	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
