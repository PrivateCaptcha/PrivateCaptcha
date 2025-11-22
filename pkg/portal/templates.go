package portal

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errTemplateNotFound  = errors.New("template with such name does not exist")
	errNoLayersInBuilder = errors.New("no template layers were added to the builder")
	errDictEvenArgs      = errors.New("dict requires even number of arguments")
	errDictKeyString     = errors.New("dict keys must be strings")
)

// FileSystemTemplateLayout holds the organized paths of templates from a single embed.FS.
type FileSystemTemplateLayout struct {
	FS           *embed.FS
	RootDir      string
	DefaultFiles []string
	Bundles      map[string][]string
	LayerName    string // For identification/debugging
}

// discoverLayout scans an embed.FS and organizes template file paths.
func discoverLayout(ctx context.Context, efs *embed.FS, templateRootDir string, layerName string) (*FileSystemTemplateLayout, error) {
	layout := &FileSystemTemplateLayout{
		FS:           efs,
		RootDir:      templateRootDir,
		DefaultFiles: []string{},
		Bundles:      make(map[string][]string),
		LayerName:    layerName,
	}
	defaultDirPath := filepath.Join(templateRootDir, "_default")

	err := fs.WalkDir(efs, templateRootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// We only care about .html files
		if !strings.HasSuffix(d.Name(), ".html") {
			return nil
		}

		dirInFS := filepath.Dir(path)
		if dirInFS == defaultDirPath {
			layout.DefaultFiles = append(layout.DefaultFiles, path)
		} else if strings.HasPrefix(dirInFS, templateRootDir+string(filepath.Separator)) {
			bundleName := strings.TrimPrefix(dirInFS, templateRootDir+string(filepath.Separator))
			layout.Bundles[bundleName] = append(layout.Bundles[bundleName], path)
		}

		return nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "Failed to traverse template files", "root", templateRootDir, "layer", layerName, common.ErrAttr(err))
		return nil, err
	}

	slog.Log(ctx, common.LevelTrace, "Discovered layout for layer", "layer", layerName, "rootDir", templateRootDir, "defaultFilesCount", len(layout.DefaultFiles), "bundleCount", len(layout.Bundles))

	return layout, nil
}

type TemplatesBuilder struct {
	layers          []*FileSystemTemplateLayout
	templateRootDir string
	functions       template.FuncMap
}

func NewTemplatesBuilder() *TemplatesBuilder {
	return &TemplatesBuilder{
		layers:          make([]*FileSystemTemplateLayout, 0),
		templateRootDir: "layouts",
		functions: template.FuncMap{
			"qescape":  url.QueryEscape,
			"title":    englishCaser.String,
			"safeHTML": func(s string) any { return template.HTML(s) },
			"safeJS":   func(s string) any { return template.JS(s) },
			"plus1":    func(x int) int { return x + 1 },
			"sub":      func(a, b int) int { return a - b },
			"dict": func(values ...interface{}) (map[string]interface{}, error) {
				if len(values)%2 != 0 {
					return nil, errDictEvenArgs
				}
				dict := make(map[string]interface{}, len(values)/2)
				for i := 0; i < len(values); i += 2 {
					key, ok := values[i].(string)
					if !ok {
						return nil, errDictKeyString
					}
					dict[key] = values[i+1]
				}
				return dict, nil
			},
			"in": func(val string, list ...string) bool {
				for _, v := range list {
					if v == val {
						return true
					}
				}
				return false
			},
			"list": func(values ...interface{}) []interface{} {
				return values
			},
		},
	}
}

func (b *TemplatesBuilder) AddFunctions(ctx context.Context, ff template.FuncMap) {
	for k, v := range ff {
		if _, ok := b.functions[k]; ok {
			slog.WarnContext(ctx, "Function is already present in the function map", "name", k)
		}
		b.functions[k] = v
	}
}

// layerName is for identification/debugging purposes.
func (b *TemplatesBuilder) AddFS(ctx context.Context, efs *embed.FS, layerName string) error {
	layout, err := discoverLayout(ctx, efs, b.templateRootDir, layerName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to discover templates layout for layer", "layer", layerName, common.ErrAttr(err))
		return err
	}

	b.layers = append(b.layers, layout)

	slog.DebugContext(ctx, "Added template filesystem layer to builder", "layer", layerName, "defaultFiles", len(layout.DefaultFiles), "bundles", len(layout.Bundles))

	return nil
}

func (b *TemplatesBuilder) Build(ctx context.Context) (*Templates, error) {
	if len(b.layers) == 0 {
		return nil, errNoLayersInBuilder
	}

	slog.InfoContext(ctx, "Building Templates object from layers", "layer_count", len(b.layers))

	// The Templates object will need the layers for its include function.
	// Make a copy to ensure immutability of the builder's state post-Build.
	finalLayers := make([]*FileSystemTemplateLayout, len(b.layers))
	copy(finalLayers, b.layers)

	templates := &Templates{
		layers:          finalLayers,
		parsedTemplates: make(map[string]*template.Template),
		// appFuncs are passed from the builder
	}

	templates.finalFuncs = template.FuncMap{
		"include": templates.includeFile,
	}

	for k, v := range b.functions {
		templates.finalFuncs[k] = v
	}

	allBundleNames := make(map[string]struct{})
	t := struct{}{}
	for _, layer := range b.layers {
		for name := range layer.Bundles {
			allBundleNames[name] = t
		}
	}

	for bundleName := range allBundleNames {
		slog.Log(ctx, common.LevelTrace, "Building bundle", "bundle", bundleName)
		// The template name for New() must be unique for this set.
		// Using bundleName is appropriate here.
		htmlTmpl := template.New(bundleName).Funcs(templates.finalFuncs)

		for _, layer := range b.layers {
			filesToParse := append([]string{}, layer.DefaultFiles...)
			if specificBundleFiles, ok := layer.Bundles[bundleName]; ok {
				filesToParse = append(filesToParse, specificBundleFiles...)
			}

			if len(filesToParse) == 0 {
				continue
			}

			slog.Log(ctx, common.LevelTrace, "Parsing files for bundle from layer",
				"bundle", bundleName, "layer", layer.LayerName, "count", len(filesToParse), "files", filesToParse)

			if _, err := htmlTmpl.ParseFS(layer.FS, filesToParse...); err != nil {
				slog.ErrorContext(ctx, "Failed to parse templates", "layer", layer.LayerName, "bundle", bundleName, common.ErrAttr(err))
				return nil, err
			}
		}

		templates.parsedTemplates[bundleName] = htmlTmpl
	}

	slog.InfoContext(ctx, "Finished building templates", "count", len(templates.parsedTemplates))

	return templates, nil
}

// Templates holds the parsed template sets and provides rendering capabilities.
// It is intended to be immutable after creation via TemplatesBuilder.Build().
type Templates struct {
	layers          []*FileSystemTemplateLayout // For the include function
	finalFuncs      template.FuncMap
	parsedTemplates map[string]*template.Template
}

func (t *Templates) includeFile(path string) template.HTML {
	for i := len(t.layers) - 1; i >= 0; i-- {
		layer := t.layers[i]
		if layer.FS == nil {
			continue
		}
		// layer.RootDir is "layouts", includePath is "settings-usage/icon.html"
		// fullPath becomes "layouts/settings-usage/icon.html"
		fullPath := filepath.Join(layer.RootDir, path)
		data, err := layer.FS.ReadFile(fullPath)
		if err == nil {
			return template.HTML(data)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Error("Error reading include file from layer", "path", path, "layer", layer.LayerName, common.ErrAttr(err))
		}
	}

	slog.Error("Failed to include template: file not found in any layer", "path", path, "layers", len(t.layers))
	return template.HTML(fmt.Sprintf("<!-- include error: %s not found -->", path))
}

// Render executes the named template with the given data.
// 'name' should be in the format "bundleName/templateFile.html" (e.g., "dashboard/index.html").
func (t *Templates) Render(ctx context.Context, w io.Writer, name string, data interface{}) error {
	bundleKey := filepath.Dir(name)

	tmpl, ok := t.parsedTemplates[bundleKey]
	if !ok {
		slog.ErrorContext(ctx, "Template bundle not found for rendering", "bundle", bundleKey, "name", name)
		return errTemplateNotFound
	}

	var buf bytes.Buffer
	templateFile := filepath.Base(name)
	slog.Log(ctx, common.LevelTrace, "About to render template", "bundle", bundleKey, "template", templateFile)
	if err := tmpl.ExecuteTemplate(&buf, templateFile, data); err != nil {
		slog.ErrorContext(ctx, "Failed to execute template", "bundle", bundleKey, "template", templateFile, common.ErrAttr(err))
		return err
	}

	if _, werr := buf.WriteTo(w); werr != nil {
		slog.ErrorContext(ctx, "Failed to write rendered template to output", common.ErrAttr(werr))
		return werr
	}

	return nil
}
