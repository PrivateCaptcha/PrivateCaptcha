package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/golang-migrate/migrate/v4"
	chmigrate "github.com/golang-migrate/migrate/v4/database/clickhouse"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/clickhouse/*.sql
var clickhouseMigrationsFS embed.FS

type ClickHouseConnectOpts struct {
	Host     string
	Database string
	User     string
	Password string
	Port     int
	Verbose  bool
}

func connectClickhouse(ctx context.Context, opts ClickHouseConnectOpts) *sql.DB {
	slog.DebugContext(ctx, "Connecting to ClickHouse", "host", opts.Host, "db", opts.Database, "user", opts.User)
	conn := clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%v", opts.Host, opts.Port)},
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.User,
			Password: opts.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout: time.Second * 30,
		//Compression: &clickhouse.Compression{
		//	Method: clickhouse.CompressionLZ4,
		//},
		Debug: opts.Verbose,
		Debugf: func(format string, v ...any) {
			slog.Log(context.TODO(), common.LevelTrace, fmt.Sprintf(format, v...), "source", "clickhouse")
		},
		//BlockBufferSize:      10,
		//MaxCompressionBuffer: 10240,
	})
	conn.SetMaxIdleConns(5)
	conn.SetMaxOpenConns(10)
	conn.SetConnMaxLifetime(time.Hour)
	return conn
}

func migrateClickhouse(ctx context.Context, db *sql.DB, dbName string) error {
	d, err := iofs.New(clickhouseMigrationsFS, "migrations/clickhouse")
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read from Clickhouse migrations IOFS", common.ErrAttr(err))
		return err
	}

	config := &chmigrate.Config{
		MigrationsTable:       chmigrate.DefaultMigrationsTable,
		MigrationsTableEngine: chmigrate.DefaultMigrationsTableEngine,
		DatabaseName:          dbName,
		ClusterName:           "",
		MultiStatementEnabled: true,
		MultiStatementMaxSize: chmigrate.DefaultMultiStatementMaxSize,
	}

	driver, err := chmigrate.WithInstance(db, config)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to connect to Clickhouse", common.ErrAttr(err))
		return err
	}

	m, err := migrate.NewWithInstance("iofs", d, "clickhouse", driver)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create migration engine for Clickhouse", common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Running Clickhouse migrations...")
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		slog.ErrorContext(ctx, "Failed to apply migrations in Clickhouse", common.ErrAttr(err))
		return err
	}

	slog.InfoContext(ctx, "Clickhouse migrated", "changes", (err != migrate.ErrNoChange))

	return nil
}
