package common

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"maps"

	"github.com/justinas/alice"
	"golang.org/x/net/xsrftoken"
)

const (
	headerHtmxRedirect = "HX-Redirect"
	maxHeaderLen       = 100
)

var (
	HeaderHtmxRequest = http.CanonicalHeaderKey("HX-Request")
	errPathArgEmpty   = errors.New("path argument is empty")
	epoch             = time.Unix(0, 0).UTC().Format(http.TimeFormat)
	// taken from chi, which took it from nginx
	NoCacheHeaders = map[string][]string{
		http.CanonicalHeaderKey("Expires"):         []string{epoch},
		http.CanonicalHeaderKey("Cache-Control"):   []string{"no-cache, no-store, no-transform, must-revalidate, private, max-age=0"},
		http.CanonicalHeaderKey("Pragma"):          []string{"no-cache"},
		http.CanonicalHeaderKey("X-Accel-Expires"): []string{"0"},
	}
	CachedHeaders = map[string][]string{
		HeaderCacheControl: []string{"public, max-age=86400"},
	}
	SecurityHeaders = map[string][]string{
		http.CanonicalHeaderKey("X-Frame-Options"):        []string{"DENY"},
		http.CanonicalHeaderKey("X-Content-Type-Options"): []string{"nosniff"},
	}
	CorsAllowAllHeaders = map[string][]string{
		HeaderAccessControlOrigin: []string{"*"},
	}
	HtmlContentHeaders = map[string][]string{
		HeaderContentType: []string{ContentTypeHTML},
	}
	JSONContentHeaders = map[string][]string{
		HeaderContentType: []string{ContentTypeJSON},
	}
	PrivateCacheControl1m  = []string{"private, max-age=60"}
	PrivateCacheControl15s = []string{"private, max-age=15"}
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

func ServiceMiddleware(svc string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(context.WithValue(r.Context(), ServiceContextKey, svc))
			next.ServeHTTP(w, r)
		})
	}
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
	maps.Copy(w.Header(), headers)
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
	if _, ok := r.Header[HeaderHtmxRequest]; ok {
		slog.Log(r.Context(), LevelTrace, "Redirecting using htmx header", "url", url)
		w.Header().Set(headerHtmxRedirect, url)
		w.WriteHeader(code)
	} else {
		slog.Log(r.Context(), LevelTrace, "Redirecting using location header", "url", url)
		w.Header().Set("Location", url)
		http.Redirect(w, r, url, http.StatusSeeOther)
	}
}

func IntPathArg(r *http.Request, name string, hasher IdentifierHasher) (int32, string, error) {
	value := r.PathValue(name)
	if len(value) == 0 {
		return 0, "", errPathArgEmpty
	}

	if hasher != nil {
		i, err := hasher.Decrypt(value)
		if (err == nil) && (i >= 0) && (i < math.MaxInt32) {
			return int32(i), value, nil
		}
		slog.ErrorContext(r.Context(), "Failed to decrypt hashed int param", "value", value, ErrAttr(err))
	}

	i, err := strconv.ParseInt(value, 10, 32)
	return int32(i), value, err
}

func StrPathArg(r *http.Request, name string) (string, error) {
	value := r.PathValue(name)

	if len(value) == 0 {
		return "", errPathArgEmpty
	}

	return value, nil
}

func CatchAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slog.WarnContext(ctx, "Inside catchall handler", "path", r.URL.Path, "method", r.Method, "host", r.Host)

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		slog.WarnContext(ctx, "Failed to handle the request", "path", r.URL.Path)

		return
	}
}

type XSRFMiddleware struct {
	Key     string
	Timeout time.Duration
}

func (xm *XSRFMiddleware) Token(userID string) string {
	return xsrftoken.Generate(xm.Key, userID, "-")
}

func (xm *XSRFMiddleware) VerifyToken(token, userID string) bool {
	return xsrftoken.ValidFor(token, xm.Key, userID, "-", xm.Timeout)
}

func GenerateETag(parts ...string) string {
	h := sha1.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{'/'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

type RouteAndHandler struct {
	pattern string
	chain   alice.Chain
	handler http.Handler
}

// RouteGenerator's point is to passthrough the path correctly to the std.Handler() of slok/go-http-metrics
// the whole magic can break if for some reason Go will not evaluate result of Route() before calling Alice's Then()
// when calling router.Handle() in setupWithPrefix()
type RouteGenerator struct {
	Prefix string
	Path   string
	routes []*RouteAndHandler
}

func (rg *RouteGenerator) Route(method string, parts ...string) string {
	rg.Path = strings.Join(parts, "/")
	result := method + " " + rg.Prefix + rg.Path
	return result
}

func (rg *RouteGenerator) Options(parts ...string) string {
	return rg.Route(http.MethodOptions, parts...)
}

func (rg *RouteGenerator) Get(parts ...string) string {
	return rg.Route(http.MethodGet, parts...)
}

func (rg *RouteGenerator) Post(parts ...string) string {
	return rg.Route(http.MethodPost, parts...)
}

func (rg *RouteGenerator) Put(parts ...string) string {
	return rg.Route(http.MethodPut, parts...)
}

func (rg *RouteGenerator) Delete(parts ...string) string {
	return rg.Route(http.MethodDelete, parts...)
}

func (rg *RouteGenerator) Patch(parts ...string) string {
	return rg.Route(http.MethodPatch, parts...)
}

func (rg *RouteGenerator) LastPath() string {
	result := rg.Path
	// side-effect: this will cause go http metrics handler to use handlerID based on request Path
	rg.Path = ""
	return result
}

func (rg *RouteGenerator) Handler(pattern string) (*RouteAndHandler, bool) {
	for _, route := range rg.routes {
		if route.pattern == pattern {
			return route, true
		}
	}

	return nil, false
}

func (rg *RouteGenerator) Handle(pattern string, chain alice.Chain, handler http.Handler) {
	if route, ok := rg.Handler(pattern); ok {
		route.chain = chain
		route.handler = handler
		return
	}

	rg.routes = append(rg.routes, &RouteAndHandler{
		pattern: pattern,
		chain:   chain,
		handler: handler,
	})
}

func (rg *RouteGenerator) Register(router *http.ServeMux) {
	for _, route := range rg.routes {
		router.Handle(route.pattern, route.chain.Then(route.handler))
	}
}
