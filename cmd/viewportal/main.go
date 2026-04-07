package main

import (
	"context"
	"encoding/json"
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
	listTemplateStart = `<html>
<body>
<strong>Portal Pages:</strong>
<ul>
`
	listTemplateEnd = `</ul>
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

func listPages(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(listTemplateStart))
	for _, p := range pages {
		_, _ = fmt.Fprintf(w, "<li><a href=\"%s\">%s</a></li>\n", p.Path, p.Path)
	}
	_, _ = w.Write([]byte(listTemplateEnd))
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

		tmpl := p.Template
		if p.ParentTemplate != "" {
			tmpl = p.ParentTemplate
		}

		out, err := srv.RenderResponse(ctx, tmpl, model, reqCtx, platformCtx)
		if err != nil {
			log.Printf("Failed to render %s: %v", tmpl, err)
			http.Error(w, fmt.Sprintf("Render error: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = out.WriteTo(w)
	}
}

func stubPropertyStatsHandler(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC()
	requested := make([]map[string]interface{}, 0, 24)
	for i := 23; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Hour)
		requested = append(requested, map[string]interface{}{
			"x": t.Unix(),
			"y": (24 - i) * 5,
		})
	}

	verified := make([]map[string]interface{}, 0, len(requested))
	for _, pt := range requested {
		verified = append(verified, map[string]interface{}{
			"x": pt["x"],
			"y": pt["y"].(int) * 80 / 100,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"requested": requested,
		"verified":  verified,
	})
}

func stubRuleStatsHandler(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC()
	usage := make([]map[string]interface{}, 0, 7)
	for i := 6; i >= 0; i-- {
		t := now.AddDate(0, 0, -i)
		usage = append(usage, map[string]interface{}{
			"x": t.Unix(),
			"y": (7 - i) * 3,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"usage": usage,
	})
}

func stubAccountStatsHandler(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC()
	data := make([]map[string]interface{}, 0, 12)
	for i := 11; i >= 0; i-- {
		t := now.AddDate(0, -i, 0)
		data = append(data, map[string]interface{}{
			"x": t.Unix(),
			"y": (12 - i) * 100,
			"s": 0,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"series": []map[string]interface{}{
			{"name": "Acme Corp", "index": 0},
		},
		"data": data,
	})
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

	sort.Slice(pages, func(i, j int) bool {
		return pages[i].Path < pages[j].Path
	})

	router := http.NewServeMux()
	router.Handle("/portal/", http.StripPrefix("/portal/", web.Static("")))

	for _, p := range pages {
		router.HandleFunc(p.Path, servePage(p))
	}

	// mock JSON endpoints for property stats, rule stats, and account stats
	statsPattern := fmt.Sprintf("/portal/%s/{%s}/%s/{%s}/%s/{%s}",
		common.OrgEndpoint, common.ParamOrg,
		common.PropertyEndpoint, common.ParamProperty,
		common.StatsEndpoint, common.ParamPeriod)
	router.HandleFunc(statsPattern, stubPropertyStatsHandler)

	ruleStatsPattern := fmt.Sprintf("/portal/%s/{%s}/%s/{%s}/%s/{%s}",
		common.OrgEndpoint, common.ParamOrg,
		common.PropertyEndpoint, common.ParamProperty,
		common.RuleStatsEndpoint, common.ParamPeriod)
	router.HandleFunc(ruleStatsPattern, stubRuleStatsHandler)

	accountStatsPattern := fmt.Sprintf("/portal/%s/%s",
		common.UserEndpoint, common.StatsEndpoint)
	router.HandleFunc(accountStatsPattern, stubAccountStatsHandler)

	// root URL and /portal/ both redirect to default org (same as real portal)
	defaultOrgPath := srv.PartsURL(common.OrgEndpoint, "org1")
	portalRoot := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, defaultOrgPath, http.StatusFound)
	}
	router.HandleFunc("/{$}", portalRoot)
	router.HandleFunc("GET /portal/{$}", portalRoot)

	// page list available at /list
	router.HandleFunc("/list", listPages)

	log.Println("Listening at http://localhost:8083/")
	log.Println("Page list at http://localhost:8083/list")
	log.Fatal(http.ListenAndServe(":8083", router))
}
