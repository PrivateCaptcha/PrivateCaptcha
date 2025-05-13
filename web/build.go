package web

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

//go:embed static
var staticFiles embed.FS

func StaticFiles() *embed.FS {
	return &staticFiles
}

func Static() http.HandlerFunc {
	sub, _ := fs.Sub(staticFiles, "static")
	srv := http.FileServer(http.FS(sub))

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "Static request", "path", r.URL.Path)
		common.WriteHeaders(w, common.CachedHeaders)
		srv.ServeHTTP(w, r)
	}
}

//go:embed layouts/*/*.html
var templateFiles embed.FS

func Templates() *embed.FS {
	return &templateFiles
}
