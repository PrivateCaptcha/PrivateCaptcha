package common

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/justinas/alice"
)

const (
	headerHtmxRedirect = "HX-Redirect"
)

var (
	headerHtmxRequest = http.CanonicalHeaderKey("HX-Request")
	errPathArgEmpty   = errors.New("path argument is empty")
	epoch             = time.Unix(0, 0).UTC().Format(http.TimeFormat)
	// taken from chi, which took it fron nginx
	NoCacheHeaders = map[string][]string{
		http.CanonicalHeaderKey("Expires"):         []string{epoch},
		http.CanonicalHeaderKey("Cache-Control"):   []string{"no-cache, no-store, no-transform, must-revalidate, private, max-age=0"},
		http.CanonicalHeaderKey("Pragma"):          []string{"no-cache"},
		http.CanonicalHeaderKey("X-Accel-Expires"): []string{"0"},
	}
	CachedHeaders = map[string][]string{
		http.CanonicalHeaderKey("Cache-Control"): []string{"public, max-age=86400"},
	}
)

func NoopMiddleware(next http.Handler) http.Handler {
	return next
}

func Recovered(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
}

func TimeoutHandler(timeout time.Duration) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer func() {
				cancel()
				if ctx.Err() == context.DeadlineExceeded {
					w.WriteHeader(http.StatusGatewayTimeout)
				}
			}()

			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(h)
	}
}

func WriteHeaders(w http.ResponseWriter, headers map[string][]string) {
	wHeader := w.Header()
	for k, v := range headers {
		wHeader[k] = v
	}
}

func Cached(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteHeaders(w, CachedHeaders)
		next.ServeHTTP(w, r)
	})
}

func NoCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteHeaders(w, NoCacheHeaders)
		next.ServeHTTP(w, r)
	})
}

func HttpStatus(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
	})
}

func Redirect(url string, code int, w http.ResponseWriter, r *http.Request) {
	if _, ok := r.Header[headerHtmxRequest]; ok {
		slog.Log(r.Context(), LevelTrace, "Redirecting using htmx header", "url", url)
		w.Header().Set(headerHtmxRedirect, url)
		w.WriteHeader(code)
	} else {
		slog.Log(r.Context(), LevelTrace, "Redirecting using location header", "url", url)
		w.Header().Set("Location", url)
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

func noContentCached(w http.ResponseWriter, r *http.Request) {
	WriteHeaders(w, CachedHeaders)
	w.WriteHeader(http.StatusNoContent)
}

func catchAll(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	slog.WarnContext(r.Context(), "CatchAll handler", "path", path, "host", r.Host, "method", r.Method)

	if strings.HasSuffix(path, "/.git/config") {
		noContentCached(w, r)
		return
	}

	if strings.HasSuffix(path, ".php") {
		noContentCached(w, r)
		return
	}

	if (len(path) > 0) && (path[0] == '/') && strings.HasPrefix(path[1:], PuzzleEndpoint) {
		noContentCached(w, r)
		return
	}

	http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
}

func robotsTXT(w http.ResponseWriter, r *http.Request) {
	contents := "User-agent: *\nDisallow: /"
	w.Header().Set(HeaderContentType, ContentTypePlain)
	WriteHeaders(w, CachedHeaders)
	fmt.Fprint(w, contents)
}

// 2xx responses make them cached on CDN level
func SetupWellKnownPaths(router *http.ServeMux, chain alice.Chain) {
	router.Handle("/robots.txt", chain.ThenFunc(robotsTXT))
	router.Handle("/favicon.ico", chain.ThenFunc(noContentCached))
	router.Handle("/sitemap.xml", chain.ThenFunc(noContentCached))
	router.Handle("/s3cmd.ini", chain.ThenFunc(noContentCached))
	router.Handle("/ads.txt", chain.ThenFunc(noContentCached))
	router.Handle("/package.json", chain.ThenFunc(noContentCached))
	router.Handle("/.well-known/", chain.ThenFunc(noContentCached))
	router.Handle("/.vscode/", chain.ThenFunc(noContentCached))
	router.Handle("/.aws/", chain.ThenFunc(noContentCached))
	router.Handle("/wp-admin/", chain.ThenFunc(noContentCached))
	router.Handle("/changelog.txt", chain.ThenFunc(noContentCached))
	router.Handle("/", chain.ThenFunc(catchAll))
}
