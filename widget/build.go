package widget

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

//go:embed static
var staticFiles embed.FS

var (
	widgetCachedHeaders = map[string][]string{
		common.HeaderCacheControl: []string{"public, max-age=300, must-revalidate"},
	}
)

func Static(gitHash string) http.HandlerFunc {
	sub, _ := fs.Sub(staticFiles, "static")
	srv := http.FileServer(http.FS(sub))

	etagHeaders := make(map[string][]string)
	if len(gitHash) > 0 {
		etagHeaders[common.HeaderETag] = []string{gitHash}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "Static request", "path", r.URL.Path)

		if etag := r.Header.Get(common.HeaderIfNoneMatch); len(etag) > 0 && (etag == gitHash) {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		common.WriteHeaders(w, widgetCachedHeaders)
		common.WriteHeaders(w, common.CorsAllowAllHeaders)
		common.WriteHeaders(w, etagHeaders)
		if r.URL.Path == "/js/privatecaptcha.js" && r.URL.Query().Get("v") == "ext" {
			r = r.Clone(r.Context())
			r.URL.Path = "/js/privatecaptcha-ext.js"
		}
		srv.ServeHTTP(w, r)
	}
}
