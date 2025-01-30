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
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	server     *Server
	cfg        *config.Config
	timeSeries *db.TimeSeriesStore
	store      *db.BusinessStore
)

type fakePuzzleEngine struct {
	result puzzle.VerifyError
}

func (f *fakePuzzleEngine) Write(ctx context.Context, p *puzzle.Puzzle, w http.ResponseWriter) error {
	return nil
}

func (f *fakePuzzleEngine) Verify(ctx context.Context, payload string, expectedOwner puzzle.OwnerIDSource, tnow time.Time) (*puzzle.Puzzle, puzzle.VerifyError, error) {
	return nil, f.result, nil
}

func TestMain(m *testing.M) {
	flag.Parse()

	paddleAPI := &billing.StubPaddleClient{}

	if testing.Short() {
		server = &Server{
			Stage:  common.StageTest,
			Prefix: "",
			XSRF:   XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
			Session: session.Manager{
				CookieName:  "pcsid",
				MaxLifetime: 1 * time.Minute,
			},
			PaddleAPI:    paddleAPI,
			PuzzleEngine: &fakePuzzleEngine{result: puzzle.VerifyNoError},
		}

		server.Init()

		os.Exit(m.Run())
	}

	common.SetupLogs(common.StageTest, true)

	var cerr error
	cfg, cerr = config.New(os.Getenv)
	if cerr != nil {
		panic(cerr)
	}

	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Connect(context.Background(), cfg)
	if dberr != nil {
		panic(dberr)
	}

	timeSeries = db.NewTimeSeries(clickhouse)

	levels := difficulty.NewLevels(timeSeries, 100, 5*time.Minute)
	defer levels.Shutdown()

	store = db.NewBusiness(pool)

	sessionStore := db.NewSessionStore(pool, memory.New(), 1*time.Minute, session.KeyPersistent)

	server = &Server{
		Stage:      common.StageTest,
		Store:      store,
		TimeSeries: timeSeries,
		Prefix:     "",
		XSRF:       XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
		Session: session.Manager{
			CookieName:  "pcsid",
			Store:       sessionStore,
			MaxLifetime: sessionStore.MaxLifetime(),
		},
		Mailer:       &email.StubMailer{},
		PaddleAPI:    paddleAPI,
		RateLimiter:  &ratelimit.StubRateLimiter{},
		PuzzleEngine: &fakePuzzleEngine{result: puzzle.VerifyNoError},
		Metrics:      monitoring.NewStub(),
	}

	server.Init()

	// TODO: seed data

	os.Exit(m.Run())
}
