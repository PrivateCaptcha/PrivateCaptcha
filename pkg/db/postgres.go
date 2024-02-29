package db

import (
	"context"
	"embed"
	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	pgMigrationsSchema = "public"
)

//go:embed migrations/postgres/*.sql
var postgresMigrationsFS embed.FS

func connectPostgres(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	slog.Debug("Connecting to Postgres...")
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create pgxpool", common.ErrAttr(err))
		return nil, err
	}

	return pool, nil
}

func migratePostgres(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

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

	slog.DebugContext(ctx, "Running Postgres migrations...")
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		slog.ErrorContext(ctx, "Failed to apply migrations in Postgres", common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Postgres migrated")

	return nil
}
