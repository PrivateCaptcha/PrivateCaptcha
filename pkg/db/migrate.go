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
	migrationsSchema = "public"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create pgxpool", common.ErrAttr(err))
		return nil, err
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	d, err := iofs.New(migrationsFS, "sql")
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read from migrations IOFS", common.ErrAttr(err))
		return nil, err
	}

	// NOTE: beware the run migrations twice problem with migrate, related to search_path
	// https://github.com/golang-migrate/migrate/blob/master/database/postgres/TUTORIAL.md#fix-issue-where-migrations-run-twice
	// the fix is to add '&search_path=public' to the connection string to force specific schema (for migrations table only)
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{SchemaName: migrationsSchema})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create migrate driver", common.ErrAttr(err))
		return nil, err
	}

	m, err := migrate.NewWithInstance("iofs", d, "postgres", driver)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create migration engine", common.ErrAttr(err))
		return nil, err
	}

	slog.DebugContext(ctx, "Running migrations...")
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		slog.ErrorContext(ctx, "Failed to apply migrations", common.ErrAttr(err))
		return nil, err
	}

	slog.InfoContext(ctx, "Database migrated")

	return pool, nil
}
