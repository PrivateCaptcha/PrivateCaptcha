package api

import (
	"log/slog"
	"net/http"
)

func Logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("Processing file request", "path", r.URL.Path, "method", r.Method)
		h.ServeHTTP(w, r)
	})
}

func Method(m string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != m {
			slog.Error("Incorrect http method", "actual", r.Method, "expected", m)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		slog.Debug("Processing API request", "path", r.URL.Path, "method", r.Method)
		next.ServeHTTP(w, r)
	}
}
