package common

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/rs/xid"
)

const (
	headerHtmxRedirect = "HX-Redirect"
)

var (
	headerHtmxRequest = http.CanonicalHeaderKey("HX-Request")
	errPathArgEmpty   = errors.New("path argument is empty")
	epoch             = time.Unix(0, 0).UTC().Format(http.TimeFormat)
	// taken from chi, which took it fron nginx
	noCacheHeaders = map[string]string{
		"Expires":         epoch,
		"Cache-Control":   "no-cache, no-store, no-transform, must-revalidate, private, max-age=0",
		"Pragma":          "no-cache",
		"X-Accel-Expires": "0",
	}
)

func traceID() string {
	return xid.New().String()
}

func Logged(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		ctx := TraceContext(r.Context(), traceID)

		slog.DebugContext(ctx, "Started request", "path", r.URL.Path, "method", r.Method)
		defer slog.DebugContext(ctx, "Finished request", "path", r.URL.Path, "method", r.Method,
			"duration", time.Since(t).Milliseconds())

		h.ServeHTTP(w, r.WithContext(ctx))
	}
}

func Recovered(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				if rvr == http.ErrAbortHandler {
					panic(rvr)
				}

				slog.ErrorContext(r.Context(), "Crash", "panic", rvr, "stack", string(debug.Stack()))

				if r.Header.Get("Connection") != "Upgrade" {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}
		}()

		next.ServeHTTP(w, r)
	}
}

func NoCache(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for k, v := range noCacheHeaders {
			w.Header().Set(k, v)
		}

		next.ServeHTTP(w, r)
	}
}

func CacheControl(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	}
}

// The reason this exists is because http.MaxBytesHandler works with http.Handler instead of http.HandlerFunc
func MaxBytesHandler(next http.HandlerFunc, maxSize int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r2 := *r
		r2.Body = http.MaxBytesReader(w, r.Body, maxSize)
		next.ServeHTTP(w, &r2)
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

func IntPathArg(r *http.Request, name string) (int, string, error) {
	value := r.PathValue(name)
	if len(value) == 0 {
		return 0, "", errPathArgEmpty
	}

	i, err := strconv.Atoi(value)
	return i, value, err
}

func StrPathArg(r *http.Request, name string) (string, error) {
	value := r.PathValue(name)

	if len(value) == 0 {
		return "", errPathArgEmpty
	}

	return value, nil
}

type Middleware func(http.HandlerFunc) http.HandlerFunc

// this exists because of https://github.com/justinas/alice/issues/25
type MiddlewareChain struct {
	handlers []Middleware
}

func NewMiddleWareChain(handlers ...Middleware) *MiddlewareChain {
	return &MiddlewareChain{
		handlers: handlers,
	}
}

func (c *MiddlewareChain) Build(final http.HandlerFunc) http.HandlerFunc {
	if len(c.handlers) == 0 {
		return final
	}

	chain := final
	for i := len(c.handlers) - 1; i >= 0; i-- {
		chain = c.handlers[i](chain)
	}

	return chain
}

func (c *MiddlewareChain) Add(m ...Middleware) *MiddlewareChain {
	return &MiddlewareChain{
		handlers: append(c.handlers, m...),
	}
}

func (c *MiddlewareChain) AddMany(other *MiddlewareChain) *MiddlewareChain {
	return &MiddlewareChain{
		handlers: append(c.handlers, other.handlers...),
	}
}
