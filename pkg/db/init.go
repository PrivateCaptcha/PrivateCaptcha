package db

import (
	"context"
	"database/sql"
	"log/slog"
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

func Connect(ctx context.Context, getenv func(string) string) (*pgxpool.Pool, *sql.DB, error) {
	connectOnce.Do(func() {
		globalPool, globalClickhouse, globalDBErr = connectEx(ctx, getenv, false /*migrate*/, false)
	})
	return globalPool, globalClickhouse, globalDBErr
}

func Migrate(ctx context.Context, getenv func(string) string, up bool) error {
	pool, clickhouse, err := connectEx(ctx, getenv, true /*migrate*/, up)
	if err != nil {
		return err
	}

	defer pool.Close()
	defer clickhouse.Close()

	return err
}

func connectEx(ctx context.Context, getenv func(string) string, migrate, up bool) (pool *pgxpool.Pool, clickhouse *sql.DB, err error) {
	verbose := getenv("VERBOSE") == "1"
	errs, ctx := errgroup.WithContext(ctx)

	errs.Go(func() error {
		opts := ClickHouseConnectOpts{
			Host:     getenv("PC_CLICKHOUSE_HOST"),
			Database: getenv("PC_CLICKHOUSE_DB"),
			User:     getenv("PC_CLICKHOUSE_USER"),
			Password: getenv("PC_CLICKHOUSE_PASSWORD"),
			Port:     9000,
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
		config, cerr := createPgxConfig(ctx, getenv, verbose)
		if cerr != nil {
			return cerr
		}

		var perr error
		pool, perr = connectPostgres(ctx, config)
		if perr != nil {
			return perr
		}
		if perr := pool.Ping(ctx); perr != nil {
			return perr
		}

		if migrate {
			domain := getenv("PC_DOMAIN")
			migrateCtx := &migrateContext{
				Stage:            getenv("STAGE"),
				PortalPropertyID: PortalPropertyID,
				PortalDomain:     "portal." + domain,
				AdminEmail:       getenv("PC_ADMIN_EMAIL"),
			}
			if len(migrateCtx.AdminEmail) == 0 {
				slog.WarnContext(ctx, "Admin email config is empty. Using domain instead")
				migrateCtx.AdminEmail = "admin@" + domain
			}
			return migratePostgres(ctx, pool, migrateCtx, up)
		}

		return nil
	})

	err = errs.Wait()

	return
}
