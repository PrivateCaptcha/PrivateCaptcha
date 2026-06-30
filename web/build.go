package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

//go:embed static
var staticFiles embed.FS

func StaticFiles() *embed.FS {
	return &staticFiles
}

// AssetVersion returns a short hash of the built CSS so dev HTML can bust browser cache.
func AssetVersion() string {
	data, err := staticFiles.ReadFile("static/css/style.css")
	if err != nil {
		return "dev"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:6])
}

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

		common.WriteHeaders(w, common.CachedHeaders)
		common.WriteHeaders(w, common.SecurityHeaders)
		common.WriteHeaders(w, common.CorsAllowAllHeaders)
		common.WriteHeaders(w, etagHeaders)
		srv.ServeHTTP(w, r)
	}
}

// StaticDev serves embedded assets without long-lived browser caching (viewportal local dev).
func StaticDev() http.HandlerFunc {
	sub, _ := fs.Sub(staticFiles, "static")
	srv := http.FileServer(http.FS(sub))

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "Static request", "path", r.URL.Path)

		common.WriteHeaders(w, common.NoCacheHeaders)
		common.WriteHeaders(w, common.SecurityHeaders)
		common.WriteHeaders(w, common.CorsAllowAllHeaders)
		srv.ServeHTTP(w, r)
	}
}

//go:embed layouts/*/*.html
var templateFiles embed.FS

func Templates() *embed.FS {
	return &templateFiles
}

//go:embed data/*.json
var dataFiles embed.FS

type DataContext map[string]interface{}

func LoadData() (DataContext, error) {
	data := make(DataContext)

	entries, err := dataFiles.ReadDir("data")
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}

		content, err := dataFiles.ReadFile("data/" + entry.Name())
		if err != nil {
			return nil, err
		}

		var parsed interface{}
		if err := json.Unmarshal(content, &parsed); err != nil {
			return nil, err
		}

		key := strings.TrimSuffix(entry.Name(), ".json")
		data[key] = parsed
	}

	return data, nil
}
