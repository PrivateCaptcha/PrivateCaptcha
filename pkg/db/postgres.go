package db

import (
	"context"
	"embed"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	pgMigrationsSchema = "public"
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
	return ctx
}

func (tracer *myQueryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		slog.Log(ctx, common.LevelTrace, "SQL command failed", common.ErrAttr(data.Err), "source", "postgres")
	}
}

func connectPostgres(ctx context.Context, dbURL string, verbose bool) (*pgxpool.Pool, error) {
	slog.Debug("Connecting to Postgres...")
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, err
	}
	if verbose {
		config.ConnConfig.Tracer = &myQueryTracer{}
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create pgxpool", common.ErrAttr(err))
		return nil, err
	}

	return pool, nil
}

func migratePostgres(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)

	d, err := iofs.New(postgresMigrationsFS, "migrations/postgres")
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read from Postgres migrations IOFS", common.ErrAttr(err))
		return err
	}

	// NOTE: beware the run migrations twice problem with migrate, related to search_path
	// https://github.com/golang-migrate/migrate/blob/master/database/postgres/TUTORIAL.md#fix-issue-where-migrations-run-twice
	// the fix is to add '&search_path=public' to the connection string to force specific schema (for migrations table only)
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{SchemaName: pgMigrationsSchema})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create migrate driver", common.ErrAttr(err))
		return err
	}

	m, err := migrate.NewWithInstance("iofs", d, "postgres", driver)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create migration engine for Postgres", common.ErrAttr(err))
		return err
	}

	defer func() {
		srcErr, dstErr := m.Close()
		if srcErr != nil {
			slog.ErrorContext(ctx, "Source error when running migrations", common.ErrAttr(srcErr))
		}
		if dstErr != nil {
			slog.ErrorContext(ctx, "Destination error when running migrations", common.ErrAttr(dstErr))
		}
		slog.DebugContext(ctx, "Closed Postgres migrate connection")
	}()

	slog.DebugContext(ctx, "Running Postgres migrations...")
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		slog.ErrorContext(ctx, "Failed to apply migrations in Postgres", common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Postgres migrated")

	return nil
}
