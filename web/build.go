package web

import (
	"context"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

//go:embed static
var staticFiles embed.FS

func Static() http.Handler {
	sub, _ := fs.Sub(staticFiles, "static")
	return http.FileServer(http.FS(sub))
}

//go:embed components/*.html components/*/*.html
var templateFiles embed.FS

type Template struct {
	templates *template.Template
}

func NewTemplates(funcs template.FuncMap) *Template {
	root := "components"
	templates := template.New("").Funcs(funcs)
	err := fs.WalkDir(templateFiles, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Read the file content
		data, err := fs.ReadFile(templateFiles, path)
		if err != nil {
			return err
		}
		// Use the relative file path as the template name, ensuring to trim any leading slash
		name := strings.TrimPrefix(path, root+"/")
		// Associate the file content with the template name
		_, err = templates.New(name).Parse(string(data))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

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
