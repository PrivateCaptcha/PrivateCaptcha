package widget

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

// StaticFiles returns the embedded assets, including whitelabel assets in enterprise builds.
func StaticFiles() *embed.FS {
	return &staticFiles
}

var (
	widgetCachedHeaders = map[string][]string{
		common.HeaderCacheControl: []string{"public, max-age=300, must-revalidate"},
	}
)

func Static(gitHash string) http.HandlerFunc {
	sub, _ := fs.Sub(staticFiles, "static")
	srv := http.FileServer(http.FS(sub))
	var whitelabelSrv http.Handler
	if enterpriseBuild {
		whitelabelSub, _ := fs.Sub(staticFiles, "whitelabel")
		whitelabelSrv = http.FileServer(http.FS(whitelabelSub))
	}

	etagHeaders := make(map[string][]string)
	if len(gitHash) > 0 {
		etagHeaders[common.HeaderETag] = []string{gitHash}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "Static request", "path", r.URL.Path)

		common.WriteHeaders(w, widgetCachedHeaders)
		common.WriteHeaders(w, common.CorsAllowAllHeaders)
		common.WriteHeaders(w, etagHeaders)
		handler := srv
		if enterpriseBuild && r.URL.Query().Get("branding") == "none" {
			handler = whitelabelSrv
			w.Header().Set(common.HeaderCacheControl, "private, no-cache, must-revalidate")
		}

		if etag := r.Header.Get(common.HeaderIfNoneMatch); len(etag) > 0 && (etag == gitHash) {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		if r.URL.Path == "/js/privatecaptcha.js" && r.URL.Query().Get("v") == "ext" {
			r = r.Clone(r.Context())
			r.URL.Path = "/js/privatecaptcha-ext.js"
		}
		handler.ServeHTTP(w, r)
	}
}
