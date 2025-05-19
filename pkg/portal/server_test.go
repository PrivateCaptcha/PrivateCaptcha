package portal

import (
	"context"
	"database/sql"
	"flag"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session/store/memory"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	server     *Server
	cfg        common.ConfigStore
	timeSeries *db.TimeSeriesDB
	store      *db.BusinessStore
	testPlan   billing.Plan
)

func portalDomain() string {
	return config.AsURL(context.TODO(), cfg.Get(common.PortalBaseURLKey)).Domain()
}

type fakePuzzleEngine struct {
	result puzzle.VerifyError
}

func (f *fakePuzzleEngine) Write(ctx context.Context, p *puzzle.Puzzle, extraSalt []byte, w http.ResponseWriter) error {
	return nil
}

func (f *fakePuzzleEngine) Verify(ctx context.Context, payload string, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.Puzzle, puzzle.VerifyError, error) {
	return nil, f.result, nil
}

func TestMain(m *testing.M) {
	flag.Parse()

	planService := billing.NewPlanService(nil)
	testPlan = planService.GetInternalTrialPlan()

	if testing.Short() {
		server = &Server{
			Stage:  common.StageTest,
			Prefix: "",
			XSRF:   &common.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
			Sessions: &session.Manager{
				CookieName:  "pcsid",
				MaxLifetime: 1 * time.Minute,
			},
			PuzzleEngine: &fakePuzzleEngine{result: puzzle.VerifyNoError},
			PlanService:  planService,
		}

		ctx := context.TODO()
		templatesBuilder := NewTemplatesBuilder()
		templatesBuilder.AddFS(ctx, web.Templates(), "core")

		if err := server.Init(ctx, templatesBuilder); err != nil {
			panic(err)
		}

		os.Exit(m.Run())
	}

	common.SetupLogs(common.StageTest, true)

	cfg = config.NewEnvConfig(config.DefaultMapper, os.Getenv)

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

	store = db.NewBusiness(pool)

	sessionStore := db.NewSessionStore(pool, memory.New(), 1*time.Minute, session.KeyPersistent)

	server = &Server{
		Stage:      common.StageTest,
		Store:      store,
		TimeSeries: timeSeries,
		Prefix:     "",
		XSRF:       &common.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
		Sessions: &session.Manager{
			CookieName:  "pcsid",
			Store:       sessionStore,
			MaxLifetime: sessionStore.MaxLifetime(),
		},
		Mailer:       &email.StubMailer{},
		Auth:         &AuthMiddleware{rateLimiter: &ratelimit.StubRateLimiter{}},
		PuzzleEngine: &fakePuzzleEngine{result: puzzle.VerifyNoError},
		Metrics:      monitoring.NewStub(),
		PlanService:  planService,
	}

	ctx := context.TODO()
	templatesBuilder := NewTemplatesBuilder()
	if err := templatesBuilder.AddFS(ctx, web.Templates(), "core"); err != nil {
		panic(err)
	}

	if err := server.Init(ctx, templatesBuilder); err != nil {
		panic(err)
	}

	// TODO: seed data

	os.Exit(m.Run())
}
