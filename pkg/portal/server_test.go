package portal

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"context"
	"database/sql"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/medama-io/go-useragent"
)

var (
	server     *Server
	cfg        common.ConfigStore
	timeSeries common.TimeSeriesStore
	store      *db.BusinessStore
	testPlan   billing.Plan
	cache      common.Cache[db.CacheKey, any]
)

func portalDomain() string {
	return config.AsURL(context.TODO(), cfg.Get(common.PortalBaseURLKey)).Domain()
}

type stubLicenseService struct{}

func (s *stubLicenseService) IsRegistered() bool {
	return false
}

func TestMain(m *testing.M) {
	flag.Parse()

	planService := billing.NewPlanService(nil)
	testPlan = planService.GetInternalTrialPlan()

	dataCtx, err := web.LoadData()
	if err != nil {
		panic(err)
	}

	cache, err = db.NewMemoryCache[db.CacheKey, any]("default", 100_000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		panic(err)
	}

	platformCtx := PlatformRenderContext{
		GitCommit:  "abcde",
		Enterprise: true,
	}
	puzzleEngine := &portal_tests.StubPuzzleEngine{Result: &puzzle.VerifyResult{Error: puzzle.VerifyNoError}}
	stubMetrics := monitoring.NewStub()

	if testing.Short() {
		store = db.NewBusinessEx(nil, cache)
		mailer := &email.StubMailer{}
		portal_tests.SetTwoFactorCodeSource(mailer.TwoFactorCode)
		server = &Server{
			Stage:  common.StageTest,
			Store:  store,
			Prefix: "",
			XSRF:   &common.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
			Sessions: &session.Manager{
				Store:       db.NewSessionStore(store, stubMetrics),
				CookieName:  "pcsid",
				MaxLifetime: 1 * time.Minute,
			},
			PuzzleEngine:       puzzleEngine,
			Mailer:             mailer,
			PlanService:        planService,
			DataCtx:            dataCtx,
			PlatformCtx:        platformCtx,
			SubscriptionLimits: &db.StubSubscriptionLimits{},
			EmailVerifier:      &PortalEmailVerifier{},
			FormURLVerifier:    api.AllowAllFormURLVerifier{},
			IDHasher:           common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "test-salt")),
			AdminEmail:         config.NewStaticValue(common.AdminEmailKey, "admin@test.com"),
		}

		ctx := context.TODO()
		templatesBuilder := NewTemplatesBuilder()
		templatesBuilder.AddFS(ctx, web.Templates(), "core")

		if err := server.Init(ctx, templatesBuilder, "", 10*time.Second); err != nil {
			panic(err)
		}

		exitCode := m.Run()
		if exitCode == 0 {
			exitCode = db_tests.TestCacheSerialization(store)
		}
		os.Exit(exitCode)
	}

	common.SetupLogs(common.StageTest, true)

	baseCfg := config.NewBaseConfig(config.NewEnvConfig(os.Getenv))
	baseCfg.Add(config.NewStaticValue(common.ClickHouseOptionalKey, "true"))
	cfg = baseCfg

	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Connect(context.Background(), cfg, 3*time.Second, false /*admin*/, nil)
	if dberr != nil {
		panic(dberr)
	}

	if clickhouse != nil {
		timeSeries = db.NewTimeSeries(clickhouse, cache)
	} else {
		timeSeries = db.NewMemoryTimeSeries()
	}

	levels := difficulty.NewLevels(timeSeries, 100, 5*time.Minute)
	levels.Init(2*time.Second, 5*time.Minute, 100*time.Millisecond)
	defer levels.Shutdown()

	store = db.NewBusinessEx(pool, cache)

	sessionStore := db.NewSessionStore(store, stubMetrics)

	ctx := context.TODO()
	cdnURLConfig := config.AsURL(ctx, cfg.Get(common.CDNBaseURLKey))
	portalURLConfig := config.AsURL(ctx, cfg.Get(common.PortalBaseURLKey))
	mailer := &email.StubMailer{Mailer: NewPortalMailer("https:"+cdnURLConfig.URL(), "https:"+portalURLConfig.URL(), &email.StubSender{}, cfg, useragent.NewParser())}
	portal_tests.SetTwoFactorCodeSource(mailer.TwoFactorCode)

	server = &Server{
		Stage:      common.StageTest,
		Store:      store,
		TimeSeries: timeSeries,
		Prefix:     "",
		XSRF:       &common.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
		Sessions: &session.Manager{
			CookieName:  "pcsid",
			Store:       sessionStore,
			MaxLifetime: sessionStore.TTL(),
		},
		Mailer:             mailer,
		RateLimiter:        &ratelimit.StubRateLimiter{Header: cfg.Get(common.RateLimitHeaderKey).Value()},
		PuzzleEngine:       puzzleEngine,
		Metrics:            stubMetrics,
		PlanService:        planService,
		DataCtx:            dataCtx,
		PlatformCtx:        platformCtx,
		IDHasher:           common.NewIDHasher(cfg.Get(common.IDHasherSaltKey)),
		AdminEmail:         cfg.Get(common.AdminEmailKey),
		CountryCodeHeader:  cfg.Get(common.CountryCodeHeaderKey),
		UserLimiter:        api.NewUserLimiter(store, planService),
		SubscriptionLimits: db.NewSubscriptionLimits(common.StageTest, store, planService),
		EmailVerifier:      &PortalEmailVerifier{},
		FormURLVerifier:    api.AllowAllFormURLVerifier{},
		LicenseService:     &stubLicenseService{},
	}

	templatesBuilder := NewTemplatesBuilder()
	if err := templatesBuilder.AddFS(ctx, web.Templates(), "core"); err != nil {
		panic(err)
	}

	store.Start(ctx, 1*time.Second)

	if err := server.Init(ctx, templatesBuilder, "", 1*time.Second); err != nil {
		panic(err)
	}

	server.UpdateConfig(ctx, cfg)

	job := &maintenance.RegisterEmailTemplatesJob{
		Templates: email.Templates(),
		Store:     store,
	}
	job.RunOnce(ctx, job.NewParams())

	exitCode := m.Run()
	if exitCode == 0 {
		exitCode = db_tests.TestCacheSerialization(store)
	}
	os.Exit(exitCode)
}

func TestPrivateRenewalFollowsAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool.Exec(ctx, "UPDATE backend.sessions SET expires_at = NOW() + INTERVAL '1 hour' WHERE session_id = $1", sid); err != nil {
		t.Fatal(err)
	}

	originalManager := server.Sessions
	freshStore := db.NewSessionStore(store, server.Metrics)
	server.Sessions = &session.Manager{
		CookieName:   originalManager.CookieName,
		Store:        freshStore,
		MaxLifetime:  originalManager.MaxLifetime,
		Path:         originalManager.Path,
		SecureCookie: originalManager.SecureCookie,
	}
	t.Cleanup(func() {
		freshStore.Stop()
		freshStore.Shutdown()
		server.Sessions = originalManager
	})

	invalidReq := httptest.NewRequest(http.MethodPut, "/settings/tab/general", nil)
	invalidReq.AddCookie(cookie)
	invalidReq.Header.Set(common.HeaderCSRFToken, "invalid")
	invalidW := httptest.NewRecorder()
	srv.ServeHTTP(invalidW, invalidReq)
	for _, responseCookie := range invalidW.Result().Cookies() {
		if responseCookie.Name == server.Sessions.CookieName {
			t.Fatal("invalid private-write CSRF renewed the session cookie")
		}
	}

	validReq := httptest.NewRequest(http.MethodGet, "/", nil)
	validReq.AddCookie(cookie)
	validW := httptest.NewRecorder()
	srv.ServeHTTP(validW, validReq)
	renewed := false
	for _, responseCookie := range validW.Result().Cookies() {
		if responseCookie.Name == server.Sessions.CookieName && responseCookie.Value == cookie.Value {
			renewed = true
		}
	}
	if !renewed {
		t.Fatal("authenticated request inside the renewal window did not refresh the cookie")
	}
}

func TestPortalServerStoreErrors(t *testing.T) {
	expectedErr := errors.New("generic db error")
	stub := &db.QuerierStub{Error: expectedErr}
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	store := db.NewBusinessWithQuerier(nil, stub, cache)

	planService := billing.NewPlanService(nil)
	dataCtx, err := web.LoadData()
	if err != nil {
		t.Fatalf("web.LoadData failed: %v", err)
	}
	platformCtx := PlatformRenderContext{
		GitCommit:  "abcde",
		Enterprise: true,
	}
	puzzleEngine := &portal_tests.StubPuzzleEngine{Result: &puzzle.VerifyResult{Error: puzzle.VerifyNoError}}
	stubMetrics := monitoring.NewStub()

	sessionStore := db.NewSessionStore(store, stubMetrics)

	ctx := context.TODO()
	baseCfg := config.NewBaseConfig(config.NewEnvConfig(os.Getenv))
	cdnURLConfig := config.AsURL(ctx, baseCfg.Get(common.CDNBaseURLKey))
	portalURLConfig := config.AsURL(ctx, baseCfg.Get(common.PortalBaseURLKey))
	mailer := NewPortalMailer("https:"+cdnURLConfig.URL(), "https:"+portalURLConfig.URL(), &email.StubSender{}, baseCfg, useragent.NewParser())

	srv := &Server{
		Stage:      common.StageTest,
		Store:      store,
		TimeSeries: db.NewMemoryTimeSeries(),
		Prefix:     "",
		XSRF:       &common.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
		Sessions: &session.Manager{
			CookieName:  "pcsid",
			Store:       sessionStore,
			MaxLifetime: sessionStore.TTL(),
		},
		Mailer:             mailer,
		RateLimiter:        &ratelimit.StubRateLimiter{Header: "X-Forwarded-For"},
		PuzzleEngine:       puzzleEngine,
		Metrics:            stubMetrics,
		PlanService:        planService,
		DataCtx:            dataCtx,
		PlatformCtx:        platformCtx,
		IDHasher:           common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "salt")),
		AdminEmail:         config.NewStaticValue(common.AdminEmailKey, "admin@test.com"),
		CountryCodeHeader:  config.NewStaticValue(common.CountryCodeHeaderKey, "CF"),
		UserLimiter:        api.NewUserLimiter(store, planService),
		SubscriptionLimits: db.NewSubscriptionLimits(common.StageTest, store, planService),
		EmailVerifier:      &PortalEmailVerifier{},
		LicenseService:     &stubLicenseService{},
	}

	templatesBuilder := NewTemplatesBuilder()
	if err := templatesBuilder.AddFS(ctx, web.Templates(), "core"); err != nil {
		t.Fatalf("AddFS failed: %v", err)
	}
	if err := srv.Init(ctx, templatesBuilder, "", 1*time.Second); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	rg := srv.Setup("/portal", common.NoopMiddleware)
	mux := http.NewServeMux()
	rg.Register(mux)

	for _, route := range rg.Routes() {
		parts := strings.SplitN(route.Prefix, " ", 2)
		var method, path string
		if len(parts) == 2 {
			method = parts[0]
			path = parts[1] + route.Path
		} else {
			method = http.MethodGet
			path = parts[0] + route.Path
		}
		path = strings.ReplaceAll(path, "{$}", "")
		path = strings.ReplaceAll(path, "{id}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{orgID}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{propertyID}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{org}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{property}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{rule}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{user}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{key}", "123")
		path = strings.ReplaceAll(path, "{period}", common.TimePeriodMonth.String())
		path = strings.ReplaceAll(path, "{tab}", common.GeneralEndpoint)
		path = strings.ReplaceAll(path, "{difficulty}", string(dbgen.DifficultyGrowthMedium))
		path = strings.ReplaceAll(path, "{code}", "429")

		req := httptest.NewRequest(method, path, nil)

		req.AddCookie(&http.Cookie{
			Name:  "pcsid",
			Value: "12345678901234567890123456789012",
		})
		req.Header.Set(common.HeaderCSRFToken, srv.XSRF.Token(""))
		req.RemoteAddr = "127.0.0.1:1234"

		w := httptest.NewRecorder()

		func(method, path string) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Route %s %s panicked: %v", method, path, r)
				}
			}()
			mux.ServeHTTP(w, req)
		}(method, path)

		if w.Code < 300 && w.Code != http.StatusNotFound && method != "OPTIONS" && !strings.HasSuffix(path, "/login") && !strings.HasSuffix(path, "/expired") {
			t.Errorf("Route %s %s expected error status, got %d", method, path, w.Code)
		}
	}
}
