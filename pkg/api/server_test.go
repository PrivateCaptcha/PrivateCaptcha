//go:build !unittests

package api

import (
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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	s          *server
	cache      common.Cache
	timeSeries *db.TimeSeriesStore
	auth       *authMiddleware
	store      *db.BusinessStore
)

func fakeRateLimiter(next http.HandlerFunc) http.HandlerFunc {
	return next
}

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs("test", true)

	var err error
	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Migrate(os.Getenv)
	if dberr != nil {
		panic(dberr)
	}

	timeSeries = db.NewTimeSeries(clickhouse)
	levels := difficulty.NewLevels(timeSeries, 100, 5*time.Minute)
	defer levels.Shutdown()

	cache, err = db.NewMemoryCache(1 * time.Minute)
	if err != nil {
		panic(err)
	}

	store = db.NewBusiness(pool, cache, 5*time.Second)
	defer store.Shutdown()

	ratelimiter, err := ratelimit.NewHTTPRateLimiter(os.Getenv(common.ConfigRateLimitHeader))
	if err != nil {
		panic(err)
	}

	auth = NewAuthMiddleware(os.Getenv, store, ratelimiter, 100*time.Millisecond)
	defer auth.Shutdown()

	s = NewServer(store, timeSeries, levels, 2*time.Second, &billing.StubPaddleClient{}, os.Getenv)
	defer s.Shutdown()

	// TODO: seed data

	os.Exit(m.Run())
}
