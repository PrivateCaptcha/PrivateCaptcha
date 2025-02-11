package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var errTemplateNotFound = errors.New("template with such name does not exist")

//go:embed static
var staticFiles embed.FS

const (
	cdnTagPortal = "portal"
)

func Static() http.HandlerFunc {
	sub, _ := fs.Sub(staticFiles, "static")
	srv := http.FileServer(http.FS(sub))

	return func(w http.ResponseWriter, r *http.Request) {
		slog.DebugContext(r.Context(), "Static request", "path", r.URL.Path)
		common.WriteHeaders(w, common.CachedHeaders)
		w.Header().Set(common.HeaderCDNTag, cdnTagPortal)
		srv.ServeHTTP(w, r)
	}
}

//go:embed layouts/*/*.html
var templateFiles embed.FS

type Template struct {
	templates map[string]*template.Template
}

func NewTemplates(ctx context.Context, funcs template.FuncMap) (*Template, error) {
	root := "layouts"
	defaultLayouts := root + "/_default"

	filesMap := make(map[string][]string, 0)

	err := fs.WalkDir(templateFiles, "layouts", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path == root {
				return nil
			}

			filesMap[path] = []string{}
			return nil
		}

		directory := filepath.Dir(path)
		filesMap[directory] = append(filesMap[directory], path)
		return nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to traverse template files", common.ErrAttr(err))
		return nil, err
	}

	baseFiles := filesMap[defaultLayouts]
	templates := make(map[string]*template.Template)

	for dir, files := range filesMap {
		if dir == defaultLayouts {
			continue
		}

		filesToParse := append(baseFiles, files...)
		name := strings.TrimPrefix(dir, root+"/")
		slog.Log(ctx, common.LevelTrace, "Parsing templates for directory", "dir", dir, "files", filesToParse)
		t := template.Must(template.New(name).Funcs(funcs).ParseFS(templateFiles, filesToParse...))
		slog.Log(ctx, common.LevelTrace, "Parsed template", "name", name, "templates", t.DefinedTemplates())
		templates[name] = t
	}

	return &Template{
		templates: templates,
	}, nil
}

// we will render templates from a single directory + "_default/" bundle every time
func (t *Template) Render(ctx context.Context, w io.Writer, name string, data interface{}) error {
	dir := filepath.Dir(name)
	tmpl, ok := t.templates[dir]
	if !ok {
		return errTemplateNotFound
	}

	var buf bytes.Buffer
	slog.Log(ctx, common.LevelTrace, "About to render template", "name", name)
	if err := tmpl.ExecuteTemplate(&buf, filepath.Base(name), data); err != nil {
		slog.ErrorContext(ctx, "Failed to execute template", "name", name, common.ErrAttr(err))
		return err
	}

	buf.WriteTo(w)
	return nil
}
