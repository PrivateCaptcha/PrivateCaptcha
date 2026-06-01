package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/PrivateCaptcha/PrivateCaptcha/widget"
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
	w.Header().Set(common.HeaderContentType, common.ContentTypeHTML)
	_, _ = w.Write([]byte(listTemplateStart))
	for _, p := range pages {
		if !p.ShowInList {
			continue
		}
		_, _ = fmt.Fprintf(w, "<li><a href=\"%s\">%s</a></li>\n", strings.TrimSuffix(p.Path, "{$}"), p.Path)
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
			API:         "//api.privatecaptcha.local",
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

		w.Header().Set(common.HeaderContentType, common.ContentTypeHTML)
		_, _ = out.WriteTo(w)
	}
}

func stubPropertyStatsHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	requested := make([]*portal.PropertyStatsPoint, 0, 24)
	for i := 23; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Hour)
		requested = append(requested, &portal.PropertyStatsPoint{
			Date:  t.Unix(),
			Value: (24 - i) * 5,
		})
	}

	verified := make([]*portal.PropertyStatsPoint, 0, len(requested))
	for _, pt := range requested {
		verified = append(verified, &portal.PropertyStatsPoint{
			Date:  pt.Date,
			Value: pt.Value * 80 / 100,
		})
	}

	common.SendJSONResponse(r.Context(), w, &portal.PropertyStatsResponse{
		Requested: requested,
		Verified:  verified,
	})
}

func stubFormStatsHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	success := make([]*portal.FormStatsPoint, 0, 24)
	for i := 23; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Hour)
		success = append(success, &portal.FormStatsPoint{
			Date:  t.Unix(),
			Value: (24 - i) * 4,
		})
	}

	failure := make([]*portal.FormStatsPoint, 0, len(success))
	for _, pt := range success {
		failure = append(failure, &portal.FormStatsPoint{
			Date:  pt.Date,
			Value: pt.Value / 4,
		})
	}

	common.SendJSONResponse(r.Context(), w, &portal.FormStatsResponse{
		Success: success,
		Failure: failure,
	})
}

func stubRuleStatsHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	usage := make([]*portal.PropertyRuleStatsPoint, 0, 7)
	for i := 6; i >= 0; i-- {
		t := now.AddDate(0, 0, -i)
		usage = append(usage, &portal.PropertyRuleStatsPoint{
			Date:  t.Unix(),
			Value: (7 - i) * 3,
		})
	}

	common.SendJSONResponse(r.Context(), w, &portal.PropertyRuleStatsResponse{
		Usage: usage,
	})
}

func stubPuzzleHandler(salt *puzzle.Salt) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		level := int(common.DifficultyLevelMedium)
		if levelStr := r.PathValue("level"); len(levelStr) > 0 {
			if value, err := strconv.Atoi(levelStr); err == nil && (value > 0) && (value < int(common.MaxDifficultyLevel)) {
				level = value
			}
		}

		p := puzzle.NewComputePuzzle(0, [16]byte{}, uint8(level))
		if err := p.Init(puzzle.DefaultValidityPeriod); err != nil {
			slog.ErrorContext(ctx, "Failed to create puzzle", common.ErrAttr(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		payload, err := p.Serialize(ctx, salt, nil)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to serialize puzzle", common.ErrAttr(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		w.Header().Set(common.HeaderContentType, common.ContentTypePlain)
		payload.Write(w)
	}
}

func stubAccountStatsHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	data := make([]*portal.AccountStatsPoint, 0, 12)
	for i := 11; i >= 0; i-- {
		t := now.AddDate(0, -i, 0)
		data = append(data, &portal.AccountStatsPoint{
			Date:   t.Unix(),
			Value:  (12 - i) * 100,
			Series: 0,
		})
	}

	common.SendJSONResponse(r.Context(), w, &portal.AccountStatsResponse{
		Series: []*portal.AccountStatsSeries{&portal.AccountStatsSeries{Name: "Acme Corp", Index: 0}},
		Data:   data,
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
	router.Handle("GET /widget/", http.StripPrefix("/widget/", widget.Static("")))

	puzzleSalt := puzzle.NewSalt([]byte("viewportal-salt"))
	puzzleHandler := stubPuzzleHandler(puzzleSalt)
	router.HandleFunc("/"+common.PuzzleEndpoint, puzzleHandler)
	router.HandleFunc("/"+common.PuzzleEndpoint+"/{level}", puzzleHandler)
	router.HandleFunc("/portal/"+common.EchoPuzzleEndpoint+"/{level}", puzzleHandler)

	for _, p := range pages {
		router.HandleFunc(p.Path, servePage(p))
	}

	// mock JSON endpoints for property stats, rule stats, and account stats
	statsPattern := fmt.Sprintf("/portal/%s/{%s}/%s/{%s}/%s/{%s}",
		common.OrgEndpoint, common.ParamOrg,
		common.PropertyEndpoint, common.ParamProperty,
		common.StatsEndpoint, common.ParamPeriod)
	router.HandleFunc(statsPattern, stubPropertyStatsHandler)

	formStatsPattern := fmt.Sprintf("/portal/%s/{%s}/%s/{%s}/%s/{%s}",
		common.OrgEndpoint, common.ParamOrg,
		common.FormEndpoint, common.ParamForm,
		common.StatsEndpoint, common.ParamPeriod)
	router.HandleFunc(formStatsPattern, stubFormStatsHandler)

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

	// page list available at /list
	router.HandleFunc("/list", listPages)

	log.Println("Listening at http://localhost:8083/")
	log.Println("Page list at http://localhost:8083/list")
	log.Fatal(http.ListenAndServe(":8083", router))
}
