package main

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

//go:embed *.html
var staticFiles embed.FS

func staticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		tmplPath := strings.TrimPrefix(path, "/")
		data, err := staticFiles.ReadFile(tmplPath)
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to read embedded file", common.ErrAttr(err))
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		extension := filepath.Ext(path)
		switch extension {
		case ".html":
			w.Header().Set("Content-Type", "text/html")
			tmpl, err := template.New("webpage").Parse(string(data))
			if err != nil {
				http.Error(w, "Failed to parse template", http.StatusInternalServerError)
				return
			}

			data := struct {
				Echo   bool
				Debug  bool
				Mode   string
				Compat string
			}{
				Echo:   r.URL.Query().Get("echo") == "true",
				Debug:  r.URL.Query().Get("debug") == "true",
				Mode:   r.URL.Query().Get("mode"),
				Compat: r.URL.Query().Get("compat"),
			}

			err = tmpl.Execute(w, &data)
			if err != nil {
				http.Error(w, "Failed to execute template", http.StatusInternalServerError)
			}
		case ".css":
			w.Header().Set("Content-Type", "text/css")
			w.Write(data)
		case ".js":
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(data)
		default:
			contentType := http.DetectContentType(data)
			w.Header().Set("Content-Type", contentType)
			w.Write(data)
		}
	})
}
