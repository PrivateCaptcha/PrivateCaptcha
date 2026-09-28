package api

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func TestGatePage(t *testing.T) {
	cache := db.NewStaticCache[db.CacheKey, any](100, &db.CacheMissingValue{})
	store := db.NewBusinessWithQuerier(nil, &db.QuerierStub{}, cache)
	sitekey := "11111111222233334444555555555555"
	property := &dbgen.Property{
		ExternalID:                db.UUIDFromSiteKey(sitekey),
		Domain:                    "example.com",
		Enabled:                   true,
		Challenge:                 dbgen.ChallengeTypeArgon2ID,
		EdgeTokenValidityInterval: time.Hour,
		EdgeWidgetStartMode:       dbgen.EdgeWidgetStartModeLoad,
	}
	s := &Server{BusinessDB: store, GatePage: NewGatePage("//cdn.privatecaptcha.com", "//api.privatecaptcha.com", "cdn.privatecaptcha.com", "api.privatecaptcha.com", false)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/gate/page?sitekey="+sitekey, nil)
	s.gatePageHandler(w, r.WithContext(context.WithValue(r.Context(), common.PropertyContextKey, property)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `data-sitekey="`+sitekey+`"`) || !strings.Contains(w.Body.String(), "//cdn.privatecaptcha.com/widget/js/privatecaptcha-ext.js") {
		t.Fatalf("gate page: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `data-start-mode="load"`) {
		t.Fatalf("cached property should use its start mode: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `<p class="site-domain">example.com</p>`) || !strings.Contains(w.Body.String(), "Complete the challenge to continue to the website.") {
		t.Fatalf("gate page must display the cached property domain: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `href="https://privatecaptcha.com"`) || !strings.Contains(w.Body.String(), `href="https://privatecaptcha.com/legal/privacy-end-user/"`) {
		t.Fatalf("gate page must link to Private Captcha and its privacy policy: %s", w.Body.String())
	}
	for k, want := range map[string]string{common.HeaderCacheControl: "public, max-age=3600", common.HeaderContentType: common.ContentTypeHTML, common.HeaderXContentTypeOptions: "nosniff", common.HeaderXRobotsTag: "noindex, nofollow", common.HeaderReferrerPolicy: "no-referrer"} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	csp := w.Header().Get(common.HeaderContentSecurityPolicy)
	for _, required := range []string{"default-src 'none'", "https://cdn.privatecaptcha.com", "https://api.privatecaptcha.com", "worker-src blob:", "'wasm-unsafe-eval'"} {
		if !strings.Contains(csp, required) {
			t.Errorf("CSP missing %q: %s", required, csp)
		}
	}
	w = httptest.NewRecorder()
	s.gatePageHandler(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "//cdn.privatecaptcha.com/widget/js/privatecaptcha.js") || !strings.Contains(w.Body.String(), `data-start-mode="click"`) {
		t.Fatalf("uncached property must receive default widget page: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `class="site-domain"`) {
		t.Fatalf("uncached property must not display a domain: %s", w.Body.String())
	}
	if err := cache.Set(t.Context(), db.PropertyBySitekeyCacheKey(sitekey), property); err != nil {
		t.Fatal(err)
	}
	property.EdgeWidgetStartMode = dbgen.EdgeWidgetStartModeClick
	w = httptest.NewRecorder()
	s.gatePageHandler(w, r)
	if !strings.Contains(w.Body.String(), "privatecaptcha-ext.js") || !strings.Contains(w.Body.String(), `data-start-mode="click"`) {
		t.Fatal("cached property did not select its widget script and start mode")
	}
}

func TestGatePageDoesNotWritePartialHTML(t *testing.T) {
	previous := GatePageTemplate
	GatePageTemplate = template.Must(template.New("broken").Parse("partial{{.DoesNotExist}}"))
	defer func() { GatePageTemplate = previous }()
	store := db.NewBusinessEx(nil, db.NewStaticCache[db.CacheKey, any](10, &db.CacheMissingValue{}))
	w := httptest.NewRecorder()
	(&Server{BusinessDB: store}).gatePageHandler(w, httptest.NewRequest(http.MethodGet, "/gate/page?sitekey=11111111222233334444555555555555", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "partial") || w.Header().Get(common.HeaderCacheControl) != "" {
		t.Fatalf("template failure leaked a cacheable partial page: status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
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
	if w := request(sitekey); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), sitekey) || !strings.Contains(w.Body.String(), `data-start-mode="click"`) {
		t.Fatalf("uncached property: %d %s", w.Code, w.Body.String())
	}
	if w := request("11111111222233334444555555555555"); w.Code != http.StatusOK {
		t.Fatalf("uncached sitekey must get page: %d", w.Code)
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
	property.EdgeWidgetStartMode = dbgen.EdgeWidgetStartModeLoad
	if w := request(sitekey); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `data-start-mode="load"`) {
		t.Fatalf("cached property did not use load mode: %d %s", w.Code, w.Body.String())
	}
	property.EdgeTokenValidityInterval = 0
	if w := request(sitekey); w.Code != http.StatusForbidden {
		t.Fatalf("non-gate property: %d", w.Code)
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
