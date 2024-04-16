package db

import (
	"context"
	"database/sql"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

var (
	connectOnce      sync.Once
	globalPool       *pgxpool.Pool
	globalClickhouse *sql.DB
	globalDBErr      error
)

func Migrate(getenv func(string) string) (*pgxpool.Pool, *sql.DB, error) {
	connectOnce.Do(func() {
		globalPool, globalClickhouse, globalDBErr = connectEx(getenv, true /*migrate*/)
	})
	return globalPool, globalClickhouse, globalDBErr
}

func connectEx(getenv func(string) string, migrate bool) (pool *pgxpool.Pool, clickhouse *sql.DB, err error) {
	verbose := getenv("VERBOSE") == "1"
	errs, ctx := errgroup.WithContext(context.Background())

	errs.Go(func() error {
		opts := ClickHouseConnectOpts{
			Host:     getenv("PC_CLICKHOUSE_HOST"),
			Database: getenv("PC_CLICKHOUSE_DB"),
			User:     getenv("PC_CLICKHOUSE_USER"),
			Password: getenv("PC_CLICKHOUSE_PASSWORD"),
			Verbose:  verbose,
		}
		clickhouse = connectClickhouse(opts)
		if perr := clickhouse.Ping(); perr != nil {
			return perr
		}

		if migrate {
			return migrateClickhouse(ctx, clickhouse, opts.Database)
		}

		return nil
	})

	errs.Go(func() error {
		var perr error
		pool, perr = connectPostgres(ctx, getenv("PC_POSTGRES"), verbose)
		if perr != nil {
			return perr
		}
		if perr := pool.Ping(ctx); perr != nil {
			return perr
		}

		if migrate {
			return migratePostgres(ctx, pool)
		}

		return nil
	})

	err = errs.Wait()

	return
}
