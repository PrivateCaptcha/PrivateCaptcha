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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session/store/memory"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	server     *Server
	cache      common.Cache
	timeSeries *db.TimeSeriesStore
	store      *db.BusinessStore
)

func fakeRateLimiter(next http.HandlerFunc) http.HandlerFunc {
	return next
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
			PaddleAPI: paddleAPI,
		}

		server.Init()

		os.Exit(m.Run())
	}

	common.SetupLogs("test", true)

	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Connect(context.Background(), os.Getenv)
	if dberr != nil {
		panic(dberr)
	}

	timeSeries = db.NewTimeSeries(clickhouse)

	levels := difficulty.NewLevels(timeSeries, 100, 5*time.Minute)
	defer levels.Shutdown()

	var err error
	cache, err = db.NewMemoryCache(1 * time.Minute)
	if err != nil {
		panic(err)
	}

	store = db.NewBusiness(pool, cache, 5*time.Second)
	defer store.Shutdown()

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
		Mailer:    &StubMailer{},
		PaddleAPI: paddleAPI,
	}

	server.Init()

	// TODO: seed data

	os.Exit(m.Run())
}
