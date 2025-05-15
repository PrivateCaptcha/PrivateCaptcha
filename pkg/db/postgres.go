package db

import (
	"context"
	"embed"
	"log/slog"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	pgMigrationsSchema                = "public"
	pgIdleInTransactionSessionTimeout = 10 * time.Second
	pgStatementTimeout                = 10 * time.Second
)

//go:embed migrations/postgres/*.sql
var postgresMigrationsFS embed.FS

type myQueryTracer struct {
}

func (tracer *myQueryTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData) context.Context {
	slog.Log(ctx, common.LevelTrace, "Starting SQL command", "sql", data.SQL, "args", data.Args, "source", "postgres")
	return context.WithValue(ctx, common.TimeContextKey, time.Now())
}

func (tracer *myQueryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		slog.Log(ctx, common.LevelTrace, "SQL command failed", common.ErrAttr(data.Err), "source", "postgres")
	} else {
		t, ok := ctx.Value(common.TimeContextKey).(time.Time)
		if !ok {
			t = time.Now()
		}
		slog.DebugContext(ctx, "SQL command finished", "source", "postgres", "duration", time.Since(t).Milliseconds())
	}
}

func postgresUser(cfg common.ConfigStore, admin bool) string {
	if admin {
		if user := cfg.Get(common.PostgresAdminKey).Value(); len(user) > 0 {
			return user
		}
	}

	return cfg.Get(common.PostgresUserKey).Value()
}

func postgresPassword(cfg common.ConfigStore, admin bool) string {
	if admin {
		if pwd := cfg.Get(common.PostgresAdminPasswordKey).Value(); len(pwd) > 0 {
			return pwd
		}
	}

	return cfg.Get(common.PostgresPasswordKey).Value()
}

func createPgxConfig(ctx context.Context, cfg common.ConfigStore, migrate bool) (config *pgxpool.Config, err error) {
	dbURL := cfg.Get(common.PostgresKey).Value()
	config, err = pgxpool.ParseConfig(dbURL)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse Postgres URL", "url", dbURL, common.ErrAttr(err))
		return nil, err
	}

	if len(dbURL) == 0 {
		config.ConnConfig.Host = cfg.Get(common.PostgresHostKey).Value()
		config.ConnConfig.Port = 5432 // Default PostgreSQL port
		config.ConnConfig.Database = cfg.Get(common.PostgresDBKey).Value()
		config.ConnConfig.User = postgresUser(cfg, migrate)
		config.ConnConfig.Password = postgresPassword(cfg, migrate)
		config.ConnConfig.TLSConfig = nil // not using SSL
	}

	config.ConnConfig.Tracer = &myQueryTracer{}

	config.ConnConfig.RuntimeParams["application_name"] = "privatecaptcha"
	config.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] =
		strconv.Itoa(int(pgIdleInTransactionSessionTimeout.Milliseconds()))
	config.ConnConfig.RuntimeParams["statement_timeout"] =
		strconv.Itoa(int(pgStatementTimeout.Milliseconds()))

	return
}

func connectPostgres(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) {
	slog.DebugContext(ctx, "Connecting to Postgres...")
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create pgxpool", common.ErrAttr(err))
		return nil, err
	}

	return pool, nil
}

type migrateContext struct {
	Stage                    string
	ExternalProductID        string
	ExternalPriceID          string
	PortalLoginPropertyID    string
	PortalRegisterPropertyID string
	PortalDomain             string
	AdminEmail               string
	PortalLoginDifficulty    common.DifficultyLevel
	PortalRegisterDifficulty common.DifficultyLevel
}

func migratePostgres(ctx context.Context, pool *pgxpool.Pool, data *migrateContext, up bool) error {
	db := stdlib.OpenDBFromPool(pool)

	tplFS := NewTemplateFS(postgresMigrationsFS, data)

	mlog := slog.With("up", up)

	d, err := iofs.New(tplFS, "migrations/postgres")
	if err != nil {
		mlog.ErrorContext(ctx, "Failed to read from Postgres migrations IOFS", common.ErrAttr(err))
		return err
	}

	// NOTE: beware the run migrations twice problem with migrate, related to search_path
	// https://github.com/golang-migrate/migrate/blob/master/database/postgres/TUTORIAL.md#fix-issue-where-migrations-run-twice
	// the fix is to add '&search_path=public' to the connection string to force specific schema (for migrations table only)
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{SchemaName: pgMigrationsSchema})
	if err != nil {
		mlog.ErrorContext(ctx, "Failed to create migrate driver", common.ErrAttr(err))
		return err
	}

	m, err := migrate.NewWithInstance("iofs", d, "postgres", driver)
	if err != nil {
		mlog.ErrorContext(ctx, "Failed to create migration engine for Postgres", common.ErrAttr(err))
		return err
	}

	defer func() {
		srcErr, dstErr := m.Close()
		if srcErr != nil {
			mlog.ErrorContext(ctx, "Source error when running migrations", common.ErrAttr(srcErr))
		}
		if dstErr != nil {
			mlog.ErrorContext(ctx, "Destination error when running migrations", common.ErrAttr(dstErr))
		}
		mlog.DebugContext(ctx, "Closed Postgres migrate connection")
	}()

	mlog.DebugContext(ctx, "Running Postgres migrations...")
	if up {
		err = m.Up()
	} else {
		err = m.Down()
	}
	if err != nil && err != migrate.ErrNoChange {
		mlog.ErrorContext(ctx, "Failed to apply migrations in Postgres", common.ErrAttr(err))
		return err
	}

	mlog.DebugContext(ctx, "Postgres migrated", "changes", (err != migrate.ErrNoChange))

	return nil
}
