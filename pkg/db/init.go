package db

import (
	"context"
	"database/sql"
	"sync"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	config_pkg "github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

var (
	connectOnce      sync.Once
	globalPool       *pgxpool.Pool
	globalClickhouse *sql.DB
	globalDBErr      error
)

func Connect(ctx context.Context, cfg common.ConfigStore) (*pgxpool.Pool, *sql.DB, error) {
	connectOnce.Do(func() {
		globalPool, globalClickhouse, globalDBErr = connectEx(ctx, cfg, nil /*admin plan*/, false /*migrate*/, false)
	})
	return globalPool, globalClickhouse, globalDBErr
}

func Migrate(ctx context.Context, cfg common.ConfigStore, adminPlan billing.Plan, up bool) error {
	pool, clickhouse, err := connectEx(ctx, cfg, adminPlan, true /*migrate*/, up)
	if err != nil {
		return err
	}

	defer pool.Close()
	defer clickhouse.Close()

	return err
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

func connectEx(ctx context.Context, cfg common.ConfigStore, adminPlan billing.Plan, migrate, up bool) (pool *pgxpool.Pool, clickhouse *sql.DB, err error) {
	errs, ctx := errgroup.WithContext(ctx)

	errs.Go(func() error {
		opts := ClickHouseConnectOpts{
			Host:     cfg.Get(common.ClickHouseHostKey).Value(),
			Database: cfg.Get(common.ClickHouseDBKey).Value(),
			User:     clickHouseUser(cfg, migrate),
			Password: clickHousePassword(cfg, migrate),
			Port:     9000,
			Verbose:  config_pkg.AsBool(cfg.Get(common.VerboseKey)),
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
			stage := cfg.Get(common.StageKey).Value()
			portalDomain := config_pkg.AsURL(ctx, cfg.Get(common.PortalBaseURLKey)).Domain()

			_, priceIDYearly := adminPlan.PriceIDs()

			migrateCtx := &migrateContext{
				Stage:                    stage,
				PortalLoginPropertyID:    PortalLoginPropertyID,
				PortalRegisterPropertyID: PortalRegisterPropertyID,
				PortalDomain:             portalDomain,
				AdminEmail:               cfg.Get(common.AdminEmailKey).Value(),
				ExternalProductID:        adminPlan.ProductID(),
				ExternalPriceID:          priceIDYearly,
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
