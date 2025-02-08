package db

import (
	"context"
	"database/sql"
	"sync"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

var (
	connectOnce      sync.Once
	globalPool       *pgxpool.Pool
	globalClickhouse *sql.DB
	globalDBErr      error
)

func Connect(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, *sql.DB, error) {
	connectOnce.Do(func() {
		globalPool, globalClickhouse, globalDBErr = connectEx(ctx, cfg, false /*migrate*/, false)
	})
	return globalPool, globalClickhouse, globalDBErr
}

func Migrate(ctx context.Context, cfg *config.Config, up bool) error {
	pool, clickhouse, err := connectEx(ctx, cfg, true /*migrate*/, up)
	if err != nil {
		return err
	}

	defer pool.Close()
	defer clickhouse.Close()

	return err
}

func connectEx(ctx context.Context, cfg *config.Config, migrate, up bool) (pool *pgxpool.Pool, clickhouse *sql.DB, err error) {
	errs, ctx := errgroup.WithContext(ctx)

	errs.Go(func() error {
		opts := ClickHouseConnectOpts{
			Host:     cfg.Getenv("PC_CLICKHOUSE_HOST"),
			Database: cfg.Getenv("PC_CLICKHOUSE_DB"),
			User:     cfg.ClickHouseUser(migrate),
			Password: cfg.ClickHousePassword(migrate),
			Port:     9000,
			Verbose:  cfg.Verbose(),
		}
		clickhouse = connectClickhouse(ctx, opts)
		if perr := clickhouse.Ping(); perr != nil {
			return perr
		}

		if migrate {
			return migrateClickhouse(common.TraceContext(ctx, "clickhouse"), clickhouse, opts.Database)
		}

		return nil
	})

	errs.Go(func() error {
		config, cerr := createPgxConfig(ctx, cfg, migrate)
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
			stage := cfg.Stage()
			adminPlan := billing.GetInternalAdminPlan()

			migrateCtx := &migrateContext{
				Stage:                    stage,
				PortalLoginPropertyID:    PortalLoginPropertyID,
				PortalRegisterPropertyID: PortalRegisterPropertyID,
				PortalDomain:             cfg.PortalDomain(),
				AdminEmail:               cfg.AdminEmail(),
				PaddleProductID:          adminPlan.PaddleProductID,
				PaddlePriceID:            adminPlan.PaddlePriceIDYearly,
				PortalLoginDifficulty:    common.DifficultyLevelSmall,
				PortalRegisterDifficulty: common.DifficultyLevelSmall,
			}

			return migratePostgres(common.TraceContext(ctx, "postgres"), pool, migrateCtx, up)
		}

		return nil
	})

	err = errs.Wait()

	return
}
