package web

import (
	"context"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

//go:embed static
var staticFiles embed.FS

func Static() http.Handler {
	_ = fs.WalkDir(staticFiles, ".", func(path string, d fs.DirEntry, _ error) error {
		if d.IsDir() {
			return nil
		}

		slog.Debug("Static filepath found", "filepath", path)

		return nil
	})
	sub, _ := fs.Sub(staticFiles, "static")
	return http.FileServer(http.FS(sub))
}

//go:embed components/*.html
var templateFiles embed.FS

type Template struct {
	templates *template.Template
}

func NewTemplates(funcs template.FuncMap) *Template {
	templates := template.Must(template.New("").Funcs(funcs).ParseFS(templateFiles, "components/*.html"))
	slog.Debug("Parsed templates", "templates", templates.DefinedTemplates())
	return &Template{
		templates: templates,
	}
}

func (t *Template) Render(ctx context.Context, w io.Writer, name string, data interface{}) error {
	tmpl, err := t.templates.Clone()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to clone templates", common.ErrAttr(err))
		return err
	}

	// we re-parse current template only so that it's blocks will be the "final" override to the blocks
	// with the same name which might be shared among the templates
	tmpl, err = tmpl.ParseFS(templateFiles, "components/"+name)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse template", "name", name, common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "About to render html template", "name", name)
	return tmpl.ExecuteTemplate(w, name, data)
}
