package portal

import (
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

	cache, err = db.NewMemoryCache[db.CacheKey, any]("default", 1000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
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
			PlanService:        planService,
			DataCtx:            dataCtx,
			PlatformCtx:        platformCtx,
			SubscriptionLimits: &db.StubSubscriptionLimits{},
			EmailVerifier:      &PortalEmailVerifier{},
		}

		ctx := context.TODO()
		templatesBuilder := NewTemplatesBuilder()
		templatesBuilder.AddFS(ctx, web.Templates(), "core")

		if err := server.Init(ctx, templatesBuilder, "", 10*time.Second); err != nil {
			panic(err)
		}

		os.Exit(m.Run())
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
	mailer := NewPortalMailer("https:"+cdnURLConfig.URL(), "https:"+portalURLConfig.URL(), &email.StubSender{}, cfg)

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
		CountryCodeHeader:  cfg.Get(common.CountryCodeHeaderKey),
		UserLimiter:        api.NewUserLimiter(store),
		SubscriptionLimits: db.NewSubscriptionLimits(common.StageTest, store, planService),
		EmailVerifier:      &PortalEmailVerifier{},
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

	os.Exit(m.Run())
}
