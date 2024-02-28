//go:build !unittests

package api

import (
	"context"
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
	"golang.org/x/sync/errgroup"
)

var (
	pool       *pgxpool.Pool
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

	errs, ctx := errgroup.WithContext(context.Background())

	errs.Go(func() error {
		var err error
		pool, err = db.ConnectPostgres(ctx, os.Getenv("PC_POSTGRES"))
		if err != nil {
			return err
		}
		if err := pool.Ping(ctx); err != nil {
			return err
		}

		return db.MigratePostgres(ctx, pool)
	})

	errs.Go(func() error {
		opts := db.ClickHouseConnectOpts{
			Host:     os.Getenv("PC_CLICKHOUSE_HOST"),
			Database: os.Getenv("PC_CLICKHOUSE_DB"),
			User:     "default",
			Password: "",
			Verbose:  os.Getenv("VERBOSE") == "1",
		}
		clickhouse = db.ConnectClickhouse(opts)
		if err := clickhouse.Ping(); err != nil {
			return err
		}

		return db.MigrateClickhouse(ctx, clickhouse, opts.Database)
	})

	if err := errs.Wait(); err != nil {
		panic(err)
	}

	levels := difficulty.NewLevels(clickhouse, 100, 5*time.Minute)
	go levels.ProcessAccessLog(context.Background(), 2*time.Second)
	go levels.BackfillDifficulty(context.Background(), 5*time.Minute)
	go levels.CleanupStats(context.Background())

	queries = dbgen.New(pool)
	cache = db.NewMemoryCache(1 * time.Minute)

	store := db.NewStore(queries, cache)
	go store.CleanupCache(context.Background(), 5*time.Second)

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
