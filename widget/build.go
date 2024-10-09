package widget

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

func Static() http.HandlerFunc {
	sub, _ := fs.Sub(staticFiles, "static")
	srv := http.FileServer(http.FS(sub))

	return func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r)
	}
}
