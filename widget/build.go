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

func Static() http.HandlerFunc {
	sub, _ := fs.Sub(staticFiles, "static")
	srv := http.FileServer(http.FS(sub))

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "Static request", "path", r.URL.Path)
		common.WriteCached(w)
		srv.ServeHTTP(w, r)
	}
}
