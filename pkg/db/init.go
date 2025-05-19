package db

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	config_pkg "github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

var (
	connectOnce          sync.Once
	globalPool           *pgxpool.Pool
	globalClickhouse     *sql.DB
	globalDBErr          error
	errConnectionTimeout = errors.New("connection timeout")
)

func Connect(ctx context.Context, cfg common.ConfigStore, timeout time.Duration, admin bool) (*pgxpool.Pool, *sql.DB, error) {
	connectOnce.Do(func() {
		globalPool, globalClickhouse, globalDBErr = connectEx(ctx, cfg, timeout, admin)
	})
	return globalPool, globalClickhouse, globalDBErr
}

func MigrateClickHouse(ctx context.Context, db *sql.DB, cfg common.ConfigStore, up bool) error {
	dbCfg := cfg.Get(common.ClickHouseDBKey)
	const migrationsTable = "private_captcha_migrations"

	return MigrateClickhouseEx(common.TraceContext(ctx, "clickhouse"), db, clickhouseMigrationsFS, dbCfg.Value(), migrationsTable)
}

func MigratePostgres(ctx context.Context, pool *pgxpool.Pool, cfg common.ConfigStore, adminPlan billing.Plan, up bool) error {
	const migrationTable = "private_captcha_migrations"

	migrateCtx := NewPostgresMigrateContext(ctx, cfg, adminPlan)
	tplFS := NewTemplateFS(postgresMigrationsFS, migrateCtx)

	return MigratePostgresEx(common.TraceContext(ctx, "postgres"), pool, tplFS, migrationTable, up)
}

func clickHouseUser(cfg common.ConfigStore, admin bool) string {
	if admin {
		if user := cfg.Get(common.ClickHouseAdminKey).Value(); len(user) > 0 {
			return user
		}
	}

	return cfg.Get(common.ClickHouseUserKey).Value()
}

func clickHousePassword(cfg common.ConfigStore, admin bool) string {
	if admin {
		if pwd := cfg.Get(common.ClickHouseAdminPasswordKey).Value(); len(pwd) > 0 {
			return pwd
		}
	}

	return cfg.Get(common.ClickHousePasswordKey).Value()
}

func connectEx(ctx context.Context, cfg common.ConfigStore, timeout time.Duration, admin bool) (pool *pgxpool.Pool, clickhouse *sql.DB, err error) {
	errs, ctx := errgroup.WithContext(ctx)

	errs.Go(func() error {
		opts := ClickHouseConnectOpts{
			Host:     cfg.Get(common.ClickHouseHostKey).Value(),
			Database: cfg.Get(common.ClickHouseDBKey).Value(),
			User:     clickHouseUser(cfg, admin),
			Password: clickHousePassword(cfg, admin),
			Port:     9000,
			Verbose:  config_pkg.AsBool(cfg.Get(common.VerboseKey)),
		}
		clickhouse = connectClickhouse(ctx, opts)
		if perr := clickhouse.Ping(); perr != nil {
			return perr
		}

		return nil
	})

	errs.Go(func() error {
		config, cerr := createPgxConfig(ctx, cfg, admin)
		if cerr != nil {
			return cerr
		}

		var perr error
		pool, perr = connectPostgres(ctx, config, timeout)
		if perr != nil {
			return perr
		}
		if perr := pool.Ping(ctx); perr != nil {
			return perr
		}

		return nil
	})

	err = errs.Wait()

	return
}
