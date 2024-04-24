//go:build !unittests

package api

import (
	"database/sql"
	"encoding/hex"
	"flag"
	"log/slog"
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
	timeSeries *db.TimeSeriesStore
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

	server = &Server{
		Auth: &AuthMiddleware{
			Store: store,
		},
		BusinessDB: store,
		Levels:     levels,
		Prefix:     "",
		Salt:       []byte("salt"),
	}

	if byteArray, err := hex.DecodeString(os.Getenv("UA_KEY")); (err == nil) && (len(byteArray) == 64) {
		copy(server.UAKey[:], byteArray[:])
	} else {
		slog.Error("Error initializing UA key for server", common.ErrAttr(err), "size", len(byteArray))
	}

	// TODO: seed data

	os.Exit(m.Run())
}
