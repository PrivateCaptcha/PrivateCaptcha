package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
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

func (opts *ClickHouseConnectOpts) Empty() bool {
	return (len(opts.Host) == 0) &&
		(len(opts.Database) == 0) &&
		(len(opts.User) == 0) &&
		(len(opts.Password) == 0)
}

func connectClickhouse(ctx context.Context, opts ClickHouseConnectOpts) *sql.DB {
	slog.DebugContext(ctx, "Connecting to ClickHouse", "host", opts.Host, "db", opts.Database, "user", opts.User)
	var chLogger *slog.Logger
	if opts.Verbose {
		chLogger = slog.Default()
	}
	options := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%v", opts.Host, opts.Port)},
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.User,
			Password: opts.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		ReadTimeout: 15 * time.Second,
		DialTimeout: 30 * time.Second,
		//Compression: &clickhouse.Compression{
		//	Method: clickhouse.CompressionLZ4,
		//},
		Logger: chLogger,
		//BlockBufferSize:      10,
		//MaxCompressionBuffer: 10240,
	}

	conn := clickhouse.OpenDB(options)
	conn.SetMaxIdleConns(5)
	conn.SetMaxOpenConns(10)
	conn.SetConnMaxLifetime(time.Hour)
	return conn
}

func MigrateClickhouseEx(ctx context.Context, db *sql.DB, migrationsFS fs.FS, dbName, tableName string, up bool) error {
	mlog := slog.With("up", up)

	d, err := iofs.New(migrationsFS, "migrations/clickhouse")
	if err != nil {
		mlog.ErrorContext(ctx, "Failed to read from Clickhouse migrations IOFS", common.ErrAttr(err))
		return err
	}

	config := &chmigrate.Config{
		MigrationsTable:       tableName,
		MigrationsTableEngine: chmigrate.DefaultMigrationsTableEngine,
		DatabaseName:          dbName,
		ClusterName:           "",
		MultiStatementEnabled: true,
		MultiStatementMaxSize: chmigrate.DefaultMultiStatementMaxSize,
	}

	driver, err := chmigrate.WithInstance(db, config)
	if err != nil {
		mlog.ErrorContext(ctx, "Failed to connect to Clickhouse", common.ErrAttr(err))
		return err
	}

	m, err := migrate.NewWithInstance("iofs", d, "clickhouse", driver)
	if err != nil {
		mlog.ErrorContext(ctx, "Failed to create migration engine for Clickhouse", common.ErrAttr(err))
		return err
	}

	slog.DebugContext(ctx, "Running Clickhouse migrations...")
	if up {
		err = m.Up()
	} else {
		err = m.Down()
	}
	if err != nil && err != migrate.ErrNoChange {
		mlog.ErrorContext(ctx, "Failed to apply migrations in Clickhouse", common.ErrAttr(err))
		return err
	}

	mlog.InfoContext(ctx, "Clickhouse migrated", "changes", (err != migrate.ErrNoChange))

	return nil
}
