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
	timeSeries *db.TimeSeriesDB
	store      *db.BusinessStore
	testPlan   billing.Plan
	cache      common.Cache[db.CacheKey, any]
)

func portalDomain() string {
	return config.AsURL(context.TODO(), cfg.Get(common.PortalBaseURLKey)).Domain()
}

func TestMain(m *testing.M) {
	flag.Parse()

	planService := billing.NewPlanService(nil)
	testPlan = planService.GetInternalTrialPlan()

	dataCtx, err := web.LoadData()
	if err != nil {
		panic(err)
	}

	cache, err = db.NewMemoryCache[db.CacheKey, any]("default", 100, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		panic(err)
	}

	platformCtx := PlatformRenderContext{
		GitCommit:  "abcde",
		Enterprise: true,
	}
	puzzleEngine := &portal_tests.StubPuzzleEngine{Result: &puzzle.VerifyResult{Error: puzzle.VerifyNoError}}

	if testing.Short() {
		store = db.NewBusinessEx(nil, cache)
		server = &Server{
			Stage:  common.StageTest,
			Store:  store,
			Prefix: "",
			XSRF:   &common.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
			Sessions: &session.Manager{
				Store:       db.NewSessionStore(store, session.KeyPersistent),
				CookieName:  "pcsid",
				MaxLifetime: 1 * time.Minute,
			},
			PuzzleEngine: puzzleEngine,
			PlanService:  planService,
			DataCtx:      dataCtx,
			PlatformCtx:  platformCtx,
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

	cfg = config.NewEnvConfig(os.Getenv)

	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Connect(context.Background(), cfg, 3*time.Second, false /*admin*/)
	if dberr != nil {
		panic(dberr)
	}

	timeSeries = db.NewTimeSeries(clickhouse)

	levels := difficulty.NewLevels(timeSeries, 100, 5*time.Minute)
	levels.Init(2*time.Second, 5*time.Minute)
	defer levels.Shutdown()

	store = db.NewBusinessEx(pool, cache)

	sessionStore := db.NewSessionStore(store, session.KeyPersistent)
	sessionStore.Start(context.Background(), 1*time.Minute)

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
		Mailer:            &email.StubMailer{},
		RateLimiter:       &ratelimit.StubRateLimiter{Header: cfg.Get(common.RateLimitHeaderKey).Value()},
		PuzzleEngine:      puzzleEngine,
		Metrics:           monitoring.NewStub(),
		PlanService:       planService,
		DataCtx:           dataCtx,
		PlatformCtx:       platformCtx,
		IDHasher:          common.NewIDHasher(cfg.Get(common.IDHasherSaltKey)),
		CountryCodeHeader: cfg.Get(common.CountryCodeHeaderKey),
		UserLimiter:       api.NewUserLimiter(store),
	}

	ctx := context.TODO()
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
