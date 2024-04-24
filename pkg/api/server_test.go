//go:build !unittests

package api

import (
	"database/sql"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	s          *server
	queries    *dbgen.Queries
	cache      common.Cache
	timeSeries *db.TimeSeriesStore
	auth       *AuthMiddleware
)

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs("test", true)

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

	queries = dbgen.New(pool)
	cache = db.NewMemoryCache(1 * time.Minute)

	store := db.NewBusiness(queries, cache, 5*time.Second)
	defer store.Shutdown()

	auth = &AuthMiddleware{Store: store}

	s = NewServer(store, timeSeries, levels, 2*time.Second, os.Getenv)
	defer s.Shutdown()

	// TODO: seed data

	os.Exit(m.Run())
}
