package common

import (
	"log/slog"
	"net/http"
)

func Logged(h http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("Processing request", "path", r.URL.Path, "method", r.Method)
		h.ServeHTTP(w, r)
	})
}

func SafeFormPost(h http.HandlerFunc, maxSize int64) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			slog.ErrorContext(r.Context(), "Incorrect http method", "actual", r.Method, "expected", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxSize)

		err := r.ParseForm()
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to read request body", ErrAttr(err))
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		h.ServeHTTP(w, r)
	})
}

func Method(m string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != m {
			slog.ErrorContext(r.Context(), "Incorrect http method", "actual", r.Method, "expected", m)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		slog.Debug("Processing request", "path", r.URL.Path, "method", r.Method)
		next.ServeHTTP(w, r)
	}
}
