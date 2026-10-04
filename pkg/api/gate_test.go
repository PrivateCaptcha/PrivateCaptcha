package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
)

func TestGatePageTemplateRenders(t *testing.T) {
	for _, domain := range []string{"", "example.com"} {
		t.Run(domain, func(t *testing.T) {
			if err := GatePageTemplate.Execute(io.Discard, struct{ Sitekey, ScriptURL, PuzzleURL, StartMode, Domain, CompletePath string }{
				"11111111222233334444555555555555", "//cdn.privatecaptcha.com/widget/js/privatecaptcha.js",
				"//api.privatecaptcha.com/puzzle", "click", domain, GateCompletePath,
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGatePageSettingsSource(t *testing.T) {
	for _, tc := range []struct {
		name            string
		contextSettings bool
		cachedSettings  bool
		cacheControl    string
	}{
		{name: "context takes priority", contextSettings: true, cacheControl: "public, max-age=300, must-revalidate"},
		{name: "cache fallback", cachedSettings: true, cacheControl: "public, max-age=300, must-revalidate"},
		{name: "missing settings", cacheControl: "no-store"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const sitekey = "11111111222233334444555555555555"
			settings := &dbgen.GetEdgeSettingsBySitekeyRow{Domain: "example.com", EdgeWidgetStartMode: dbgen.EdgeWidgetStartModeLoad}
			r := httptest.NewRequest(http.MethodGet, "/gate/page?sitekey="+sitekey, nil)
			s := &Server{}
			if tc.contextSettings {
				r = r.WithContext(context.WithValue(r.Context(), common.EdgeSettingsContextKey, settings))
			} else {
				cache := db.NewStaticCache[db.CacheKey, any](10, &db.CacheMissingValue{})
				if tc.cachedSettings {
					if err := cache.Set(t.Context(), db.EdgeSettingsBySitekeyCacheKey(sitekey), settings); err != nil {
						t.Fatal(err)
					}
				}
				s.BusinessDB = db.NewBusinessEx(nil, cache)
			}
			w := httptest.NewRecorder()
			s.gatePageHandler(w, r)
			if w.Code != http.StatusOK || w.Header().Get(common.HeaderCacheControl) != tc.cacheControl {
				t.Fatalf("gate response status = %d, cache control = %q, want %q", w.Code, w.Header().Get(common.HeaderCacheControl), tc.cacheControl)
			}
		})
	}
}

func TestGateSitekeyBackfillRouting(t *testing.T) {
	sitekey := "11111111222233334444555555555555"
	for _, tc := range []struct {
		name             string
		property         *dbgen.Property
		edge             bool
		negative         bool
		propertyNegative bool
		status           int
		edgeQueued       int
		propertyQueued   int
	}{
		{name: "cold caches", status: http.StatusOK, edgeQueued: 1, propertyQueued: 1},
		{name: "cached enabled property", property: &dbgen.Property{Enabled: true, EdgeTokenValidityInterval: time.Hour}, status: http.StatusOK, edgeQueued: 1},
		{name: "cached disabled edge", property: &dbgen.Property{Enabled: true}, status: http.StatusOK, edgeQueued: 1},
		{name: "cached disabled property", property: &dbgen.Property{EdgeTokenValidityInterval: time.Hour}, status: http.StatusForbidden},
		{name: "cached edge settings", edge: true, status: http.StatusOK},
		{name: "negative edge cache", negative: true, status: http.StatusOK, propertyQueued: 1},
		{name: "missing settings for enabled property", negative: true, property: &dbgen.Property{Enabled: true, EdgeTokenValidityInterval: time.Hour}, status: http.StatusOK},
		{name: "negative property cache", propertyNegative: true, status: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := db.NewStaticCache[db.CacheKey, any](10, &db.CacheMissingValue{})
			store := db.NewBusinessEx(nil, cache)
			if tc.property != nil {
				if err := cache.Set(t.Context(), db.PropertyBySitekeyCacheKey(sitekey), tc.property); err != nil {
					t.Fatal(err)
				}
			}
			if tc.edge {
				if err := cache.Set(t.Context(), db.EdgeSettingsBySitekeyCacheKey(sitekey), &dbgen.GetEdgeSettingsBySitekeyRow{Domain: "example.com"}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.negative {
				if err := cache.SetMissing(t.Context(), db.EdgeSettingsBySitekeyCacheKey(sitekey)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.propertyNegative {
				if err := cache.SetMissing(t.Context(), db.PropertyBySitekeyCacheKey(sitekey)); err != nil {
					t.Fatal(err)
				}
			}
			auth := &AuthMiddleware{Store: store, SitekeyChan: make(chan string, 1), EdgeChan: make(chan string, 1), Metrics: monitoring.NewStub(), backpressureTimeout: time.Second}
			w := httptest.NewRecorder()
			auth.GateSitekey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Context().Value(common.SitekeyContextKey) != sitekey {
					t.Fatal("gate request is missing sitekey context")
				}
				settings, _ := r.Context().Value(common.EdgeSettingsContextKey).(*dbgen.GetEdgeSettingsBySitekeyRow)
				if (settings != nil) != tc.edge {
					t.Fatalf("edge settings context = %+v, want cached settings %v", settings, tc.edge)
				}
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gate/page?sitekey="+sitekey, nil))
			if w.Code != tc.status || len(auth.EdgeChan) != tc.edgeQueued || len(auth.SitekeyChan) != tc.propertyQueued {
				t.Fatalf("status=%d edge queued=%d property queued=%d", w.Code, len(auth.EdgeChan), len(auth.SitekeyChan))
			}
		})
	}
}

func TestSitekeyMiddlewareStillRequiresOriginForGatePath(t *testing.T) {
	cache := db.NewStaticCache[db.CacheKey, any](10, &db.CacheMissingValue{})
	sitekey := "11111111222233334444555555555555"
	if err := cache.SetMissing(t.Context(), db.PropertyBySitekeyCacheKey(sitekey)); err != nil {
		t.Fatal(err)
	}
	auth := &AuthMiddleware{Store: db.NewBusinessEx(nil, cache)}
	w := httptest.NewRecorder()
	auth.Sitekey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gate/page?sitekey="+sitekey, nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Sitekey without Origin = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGatePagePropertyLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	params := db_tests.CreateNewPropertyParams(user.ID, "gate.example.com")
	params.EdgeTokenValidityInterval = time.Hour
	property, _, err := store.Impl().CreateNewProperty(ctx, params, org)
	if err != nil {
		t.Fatal(err)
	}
	sitekey := db.UUIDToSiteKey(property.ExternalID)
	store.Cache.Delete(ctx, db.PropertyBySitekeyCacheKey(sitekey))
	router := http.NewServeMux()
	server.Setup("", false, common.NoopMiddleware).Register(router)
	request := func(key string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gate/page?sitekey="+key, nil))
		return w
	}
	if w := request(sitekey); w.Code != http.StatusOK {
		t.Fatalf("uncached property: %d", w.Code)
	}
	if err := store.Cache.Set(ctx, db.PropertyBySitekeyCacheKey(sitekey), property); err != nil {
		t.Fatal(err)
	}
	if err := server.Auth.backfillEdgeImpl(ctx, map[string]uint{sitekey: 1}); err != nil {
		t.Fatal(err)
	}
	if w := request(sitekey); w.Code != http.StatusOK {
		t.Fatalf("missing edge settings: %d", w.Code)
	}
	if w := request("11111111222233334444555555555555"); w.Code != http.StatusOK {
		t.Fatalf("uncached sitekey must get page: %d", w.Code)
	}
	if err := store.Cache.SetMissing(ctx, db.EdgeSettingsBySitekeyCacheKey("11111111222233334444555555555555")); err != nil {
		t.Fatal(err)
	}
	if err := store.Cache.SetMissing(ctx, db.PropertyBySitekeyCacheKey("11111111222233334444555555555555")); err != nil {
		t.Fatal(err)
	}
	if w := request("11111111222233334444555555555555"); w.Code != http.StatusForbidden {
		t.Fatalf("negative-cached sitekey: %d", w.Code)
	}
	if err := store.Cache.Set(ctx, db.PropertyBySitekeyCacheKey(sitekey), property); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Impl().UpdateEdgeSettings(ctx, org, user, property, dbgen.EdgeWidgetStartModeLoad); err != nil {
		t.Fatal(err)
	}
	if err := server.Auth.backfillEdgeImpl(ctx, map[string]uint{sitekey: 1}); err != nil {
		t.Fatal(err)
	}
	if w := request(sitekey); w.Code != http.StatusOK || w.Header().Get(common.HeaderCacheControl) != "public, max-age=300, must-revalidate" {
		t.Fatalf("cached property: status=%d headers=%v", w.Code, w.Header())
	}
	property.EdgeTokenValidityInterval = 0
	store.Cache.Delete(ctx, db.EdgeSettingsBySitekeyCacheKey(sitekey))
	if w := request(sitekey); w.Code != http.StatusOK || w.Header().Get(common.HeaderCacheControl) != "no-store" {
		t.Fatalf("non-gate property: status=%d headers=%v", w.Code, w.Header())
	}
	property.EdgeTokenValidityInterval = time.Hour
	property.Enabled = false
	if w := request(sitekey); w.Code != http.StatusForbidden {
		t.Fatalf("disabled property: %d", w.Code)
	}
	if w := request("garbage"); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid sitekey: %d", w.Code)
	}
}
