package db

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

func Connect(getenv func(string) string) (pool *pgxpool.Pool, clickhouse *sql.DB, err error) {
	errs, ctx := errgroup.WithContext(context.Background())

	errs.Go(func() error {
		opts := ClickHouseConnectOpts{
			Host:     getenv("PC_CLICKHOUSE_HOST"),
			Database: getenv("PC_CLICKHOUSE_DB"),
			User:     "default",
			Password: "",
			Verbose:  getenv("VERBOSE") == "1",
		}
		clickhouse = connectClickhouse(opts)
		if perr := clickhouse.Ping(); perr != nil {
			return perr
		}

		return migrateClickhouse(ctx, clickhouse, opts.Database)
	})

	errs.Go(func() error {
		var perr error
		pool, perr = connectPostgres(ctx, getenv("PC_POSTGRES"))
		if perr != nil {
			return perr
		}
		if perr := pool.Ping(ctx); perr != nil {
			return err
		}

		return migratePostgres(ctx, pool)
	})

	err = errs.Wait()

	return
}
