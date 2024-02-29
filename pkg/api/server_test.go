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
	server     *Server
	queries    *dbgen.Queries
	cache      common.Cache
	clickhouse *sql.DB
)

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs("test", true)

	var pool *pgxpool.Pool
	var dberr error
	pool, clickhouse, dberr = db.Connect(os.Getenv)
	if dberr != nil {
		panic(dberr)
	}

	levels := difficulty.NewLevels(clickhouse, 100, 5*time.Minute)
	defer levels.Shutdown()

	queries = dbgen.New(pool)
	cache = db.NewMemoryCache(1 * time.Minute)

	store := db.NewStore(queries, cache, 5*time.Second)
	defer store.Shutdown()

	server = &Server{
		Auth: &AuthMiddleware{
			Store: store,
		},
		Store:  store,
		Levels: levels,
		Prefix: "",
		Salt:   []byte("salt"),
	}

	// TODO: seed data

	os.Exit(m.Run())
}
