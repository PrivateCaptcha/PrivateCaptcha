package common

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rs/xid"
)

const (
	headerHtmxRedirect = "HX-Redirect"
)

var (
	headerHtmxRequest = http.CanonicalHeaderKey("HX-Request")
)

func traceID() string {
	return xid.New().String()
}

func Logged(h http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := TraceContext(r.Context(), traceID)
		slog.DebugContext(ctx, "Processing request", "path", r.URL.Path, "method", r.Method)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func SafeFormPost(h http.HandlerFunc, maxSize int64) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		slog.DebugContext(r.Context(), "Processing request", "path", r.URL.Path, "method", r.Method)
		next.ServeHTTP(w, r)
	}
}

func Redirect(url string, w http.ResponseWriter, r *http.Request) {
	if _, ok := r.Header[headerHtmxRequest]; ok {
		slog.Log(r.Context(), LevelTrace, "Redirecting using htmx header", "url", url)
		w.Header().Set(headerHtmxRedirect, url)
		w.WriteHeader(http.StatusOK)
	} else {
		w.Header().Set("Location", url)
		// w.Header().Set("Cache-Control", "public, max-age=86400")
		http.Redirect(w, r, url, http.StatusSeeOther)
	}
}

func IntArg(next http.HandlerFunc, name string, key ContextKey, failURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value := r.PathValue(name)

		i, err := strconv.Atoi(value)
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to parse path parameter", "name", name, "value", value, ErrAttr(err))
			Redirect(failURL, w, r)
			return
		}

		ctx := context.WithValue(r.Context(), key, i)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func StrArg(next http.HandlerFunc, name string, key ContextKey, failURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value := r.PathValue(name)

		if len(value) == 0 {
			slog.ErrorContext(r.Context(), "Path parameter is empty", "name", name)
			Redirect(failURL, w, r)
			return
		}

		ctx := context.WithValue(r.Context(), key, value)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
