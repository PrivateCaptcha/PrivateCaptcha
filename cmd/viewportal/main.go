package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

const (
	rootTemplateStart = `<html>
<body>
<strong>Portal Pages:</strong>
<ul>
`
	rootTemplateEnd = `</ul>
</body>
</html>`
)

// stubSessionStore implements session.Store with no-ops.
type stubSessionStore struct{}

func (s *stubSessionStore) Start(_ context.Context, _ time.Duration) {}
func (s *stubSessionStore) Init(_ context.Context, _ *session.Session) error {
	return nil
}
func (s *stubSessionStore) Read(_ context.Context, _ string, _ bool) (*session.Session, error) {
	return nil, fmt.Errorf("not found")
}
func (s *stubSessionStore) Update(_ context.Context, _ *session.Session) error { return nil }
func (s *stubSessionStore) Destroy(_ context.Context, _ string) error          { return nil }

var (
	srv   *portal.Server
	pages []portal.ViewPortalPage
)

func alertFromQuery(r *http.Request) portal.AlertRenderContext {
	return portal.AlertRenderContext{
		InfoMessage:    r.URL.Query().Get("info"),
		WarningMessage: r.URL.Query().Get("warn"),
		SuccessMessage: r.URL.Query().Get("success"),
		ErrorMessage:   r.URL.Query().Get("error"),
	}
}

func enterpriseFromQuery(r *http.Request) bool {
	v := r.URL.Query().Get("enterprise")
	if v == "" {
		return true
	}
	return v != "false" && v != "0"
}

func homepage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(rootTemplateStart))
	for _, p := range pages {
		_, _ = fmt.Fprintf(w, "<li><a href=\"%s\">%s</a></li>\n", p.Path, p.Path)
	}
	_, _ = w.Write([]byte(rootTemplateEnd))
}

func servePage(p portal.ViewPortalPage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		alert := alertFromQuery(r)
		model := p.ModelFunc(alert)

		reqCtx := &portal.RequestContext{
			Path:        r.URL.Path,
			CurrentYear: time.Now().Year(),
			UserName:    "Jane Doe",
			UserEmail:   "jane@example.com",
			LoggedIn:    true,
		}

		platformCtx := &portal.PlatformRenderContext{
			GitCommit:  "viewportal",
			Enterprise: enterpriseFromQuery(r),
		}

		out, err := srv.RenderResponse(ctx, p.Template, model, reqCtx, platformCtx)
		if err != nil {
			log.Printf("Failed to render %s: %v", p.Template, err)
			http.Error(w, fmt.Sprintf("Render error: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = out.WriteTo(w)
	}
}

func main() {
	ctx := context.Background()

	dataCtx, err := web.LoadData()
	if err != nil {
		log.Fatalf("Failed to load data: %v", err)
	}

	srv = &portal.Server{
		Prefix:   "/portal",
		APIURL:   "http://localhost:8080/api",
		Stage:    common.StageDev,
		Sessions: &session.Manager{Store: &stubSessionStore{}},
		DataCtx:  dataCtx,
		XSRF:     &common.XSRFMiddleware{Key: "viewportal-key", Timeout: 1 * time.Hour},
	}

	builder := portal.NewTemplatesBuilder()
	if err := builder.AddFS(ctx, web.Templates(), "core"); err != nil {
		log.Fatalf("Failed to add template FS: %v", err)
	}

	if err := srv.Init(ctx, builder, "viewportal", 1*time.Hour); err != nil {
		log.Fatalf("Failed to init portal server: %v", err)
	}

	pages = srv.BuildViewPortalPages()

	router := http.NewServeMux()
	router.HandleFunc("/", homepage)
	router.Handle("/portal/", http.StripPrefix("/portal/", web.Static("")))

	sorted := make([]portal.ViewPortalPage, len(pages))
	copy(sorted, pages)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].Path) > len(sorted[j].Path)
	})

	for _, p := range sorted {
		router.HandleFunc(p.Path, servePage(p))
	}

	log.Println("Listening at http://localhost:8083/")
	log.Fatal(http.ListenAndServe(":8083", router))
}
