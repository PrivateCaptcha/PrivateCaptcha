package portal

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinas/alice"
	"github.com/medama-io/go-useragent"
)

var (
	server     *Server
	cfg        common.ConfigStore
	timeSeries common.TimeSeriesStore
	store      *db.BusinessStore
	testPlan   billing.Plan
	cache      common.Cache[db.CacheKey, any]
	testMailer *email.StubMailer
)

type unavailableSessionStore struct {
	session.Store
	err error
}

type trackedSessionStore struct {
	session.Store
	sess           *session.Session
	renewalClaimed bool
}

func responseCookieForTest(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response does not contain cookie %q", name)
	return nil
}

func invalidVerificationCode(code int) int {
	wrongCode := code + 1
	if wrongCode > 999999 {
		return code - 1
	}
	return wrongCode
}

func newMiddlewareTestServer(store session.Store) *Server {
	return &Server{
		Prefix:      "/portal",
		Metrics:     monitoring.NewStub(),
		RateLimiter: &ratelimit.StubRateLimiter{},
		Sessions: &session.Manager{
			CookieName:  "pcsid",
			Store:       store,
			MaxLifetime: 3 * time.Hour,
			Path:        "/portal",
		},
	}
}

func (s *unavailableSessionStore) Read(context.Context, string, bool) (*session.Session, error) {
	return nil, s.err
}

func (s *trackedSessionStore) Read(context.Context, string, bool) (*session.Session, error) {
	return s.sess, nil
}

func (s *trackedSessionStore) Update(_ context.Context, _ *session.Session, update func()) error {
	update()
	return nil
}

func (s *trackedSessionStore) RenewExpiration(context.Context, *session.Session) bool {
	return s.renewalClaimed
}

func TestPrivateSessionStoreErrorReturnsServiceUnavailableWithoutCookie(t *testing.T) {
	const handlerMarker = "private handler reached"
	expectedErr := errors.New("database unavailable")
	srv := newMiddlewareTestServer(&unavailableSessionStore{err: expectedErr})
	handler := srv.private(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(handlerMarker)) }))
	req := httptest.NewRequest(http.MethodGet, "/portal/settings", nil)
	req.AddCookie(&http.Cookie{Name: "pcsid", Value: "authenticated-session"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(w.Body.String(), handlerMarker) {
		t.Fatal("private handler ran while the session store was unavailable")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("store failure replaced the existing cookie")
	}
}

func TestRegistrationProcessingCookieAcrossReplicas(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	sid := t.Name() + "-processing"
	processingData := session.NewSessionData(sid)
	processingSession := session.NewSession(processingData, &trackedSessionStore{})
	if err := processingSession.Set(ctx, session.KeyLoginStep, loginStepCompleted); err != nil {
		t.Fatal(err)
	}
	processingPayload, err := processingData.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	queries := dbgen.New(store.Pool)
	if _, err := queries.CreateSession(ctx, &dbgen.CreateSessionParams{
		SessionID: sid,
		State:     dbgen.SessionStateRegistrationProcessing,
		Data:      processingPayload,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	const handlerMarker = "private handler reached"
	replica, _ := newChallengeReplica(t)
	handler := replica.private(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(handlerMarker)) }))
	request := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/settings", nil)
		req.AddCookie(&http.Cookie{Name: replica.Sessions.CookieName, Value: sid})
		return req
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request())
	if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
		t.Fatalf("remote processing request = (status %d, cookies %d)", w.Code, len(w.Result().Cookies()))
	}
	if strings.Contains(w.Body.String(), handlerMarker) {
		t.Fatal("remote processing session reached the private handler")
	}

	authenticatedData := session.NewSessionData(sid)
	authenticatedSession := session.NewSession(authenticatedData, &trackedSessionStore{})
	if err := authenticatedSession.Set(ctx, session.KeyLoginStep, loginStepCompleted); err != nil {
		t.Fatal(err)
	}
	if err := authenticatedSession.Set(ctx, session.KeyUserID, user.ID); err != nil {
		t.Fatal(err)
	}
	authenticatedPayload, err := authenticatedData.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queries.FinalizeRegistrationSession(ctx, &dbgen.FinalizeRegistrationSessionParams{
		UserID:          user.ID,
		Data:            authenticatedPayload,
		SessionID:       sid,
		ExpectedVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request())
	if !strings.Contains(w.Body.String(), handlerMarker) {
		t.Fatalf("remote finalized request did not reach the private handler: status %d", w.Code)
	}
}

func TestPrivateWriteRejectsCSRFFailureBeforeExpirationRefresh(t *testing.T) {
	const handlerMarker = "private handler reached"
	tracked := &trackedSessionStore{renewalClaimed: true}
	now := time.Now()
	sd := session.NewSessionData("authenticated-session")
	sd.SetAuthority(session.StateAuthenticated, 1, now.Add(2*time.Hour), now)
	tracked.sess = session.NewSession(sd, tracked)
	if err := tracked.sess.Set(t.Context(), session.KeyLoginStep, loginStepCompleted); err != nil {
		t.Fatal(err)
	}
	if err := tracked.sess.Set(t.Context(), session.KeyUserID, int32(1)); err != nil {
		t.Fatal(err)
	}
	srv := newMiddlewareTestServer(tracked)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(handlerMarker)) })
	handler := srv.MiddlewarePrivateWrite(alice.New(), common.NoopMiddleware).Then(next)
	req := httptest.NewRequest(http.MethodPost, "/portal/settings", nil)
	req.AddCookie(&http.Cookie{Name: "pcsid", Value: tracked.sess.ID()})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), handlerMarker) {
		t.Fatal("private write handler ran without a CSRF token")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("CSRF rejection refreshed the session cookie")
	}
}

func TestPrivateReadRefreshesAuthenticatedExpiration(t *testing.T) {
	const handlerMarker = "private handler reached"
	tracked := &trackedSessionStore{renewalClaimed: true}
	now := time.Now()
	sd := session.NewSessionData("authenticated-session")
	sd.SetAuthority(session.StateAuthenticated, 1, now.Add(2*time.Hour), now)
	tracked.sess = session.NewSession(sd, tracked)
	if err := tracked.sess.Set(t.Context(), session.KeyLoginStep, loginStepCompleted); err != nil {
		t.Fatal(err)
	}
	srv := newMiddlewareTestServer(tracked)
	handler := srv.MiddlewarePrivateRead(alice.New(), common.NoopMiddleware).Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(handlerMarker))
	}))
	req := httptest.NewRequest(http.MethodGet, "/portal/settings", nil)
	req.AddCookie(&http.Cookie{Name: "pcsid", Value: tracked.sess.ID()})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), handlerMarker) {
		t.Fatalf("private read did not reach the handler: status %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != tracked.sess.ID() {
		t.Fatal("private read did not refresh the same session cookie")
	}
}

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
		testMailer = &email.StubMailer{}
		portal_tests.SetSignInCodeSource(testMailer.TwoFactorCode)
		store = db.NewBusinessEx(nil, cache)
		server = &Server{
			Stage:  common.StageTest,
			Store:  store,
			Prefix: "",
			XSRF:   &common.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
			Sessions: &session.Manager{
				Store:       db.NewSessionStore(store, session.KeyPersistent, stubMetrics),
				CookieName:  "pcsid",
				MaxLifetime: 1 * time.Minute,
			},
			PuzzleEngine:       puzzleEngine,
			Mailer:             testMailer,
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

	sessionStore := db.NewSessionStore(store, session.KeyPersistent, stubMetrics)

	ctx := context.TODO()
	cdnURLConfig := config.AsURL(ctx, cfg.Get(common.CDNBaseURLKey))
	portalURLConfig := config.AsURL(ctx, cfg.Get(common.PortalBaseURLKey))
	mailer := NewPortalMailer("https:"+cdnURLConfig.URL(), "https:"+portalURLConfig.URL(), &email.StubSender{}, cfg, useragent.NewParser())
	testMailer = &email.StubMailer{Mailer: mailer}
	portal_tests.SetSignInCodeSource(testMailer.TwoFactorCode)

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
		Mailer:             testMailer,
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

	sessionStore := db.NewSessionStore(store, session.KeyPersistent, stubMetrics)

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
