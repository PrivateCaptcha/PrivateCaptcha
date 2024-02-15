package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

var (
	GitCommit  string
	pool       *pgxpool.Pool // Postgres will be needed by http server for dashboard and API
	cache      common.Cache  // cache will be needed by everybody
	clickhouse *sql.DB       // clickhouse will be needed by API server and dashboard
)

const (
	propertyBucketSize = 5 * time.Minute
	levelsBatchSize    = 100
)

func main() {
	stage := os.Getenv("STAGE")
	common.SetupLogs(stage, os.Getenv("VERBOSE") == "1")

	errs, ctx := errgroup.WithContext(context.Background())

	errs.Go(func() error {
		opts := db.ClickHouseConnectOpts{
			Host:     os.Getenv("PC_CLICKHOUSE_HOST"),
			Database: os.Getenv("PC_CLICKHOUSE_DB"),
			User:     "default",
			Password: "",
			Verbose:  os.Getenv("VERBOSE") == "1",
		}
		clickhouse = db.ConnectClickhouse(opts)
		if err := clickhouse.Ping(); err != nil {
			return err
		}

		return db.MigrateClickhouse(ctx, clickhouse, opts.Database)
	})

	errs.Go(func() error {
		var err error
		pool, err = db.ConnectPostgres(ctx, os.Getenv("PC_POSTGRES"))
		if err != nil {
			return err
		}
		if err := pool.Ping(ctx); err != nil {
			return err
		}

		return db.MigratePostgres(ctx, pool)
	})

	errs.Go(func() error {
		opts, err := redis.ParseURL(os.Getenv("PC_REDIS"))
		if err != nil {
			return err
		}

		redis := db.NewRedisCache(opts)
		cache = redis
		return redis.Ping(ctx)
	})

	if err := errs.Wait(); err != nil {
		panic(err)
	}

	defer pool.Close()
	defer clickhouse.Close()

	store := db.NewStore(dbgen.New(pool), cache)

	levels := difficulty.NewLevels(clickhouse, levelsBatchSize, propertyBucketSize)
	// TODO: Cancel context during graceful shutdown
	go levels.ProcessAccessLog(context.Background(), 2*time.Second)
	go levels.BackfillDifficulty(context.Background(), propertyBucketSize)
	go levels.CleanupStats()

	server := &api.Server{
		Auth: &api.AuthMiddleware{
			Store: store,
		},
		Store:  store,
		Levels: levels,
		Prefix: "api",
		Salt:   []byte("salt"),
	}

	router := http.NewServeMux()

	router.Handle("/", api.Logged(web.Handler()))
	server.Setup(router)

	host := os.Getenv("PC_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("PC_PORT")
	if port == "" {
		port = "8080"
	}

	slog.Info("Listening", "address", fmt.Sprintf("http://%v:%v", host, port), "version", GitCommit)

	s := &http.Server{
		Addr:              host + ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 4 * time.Second,
		ReadTimeout:       10 * time.Second,
		MaxHeaderBytes:    256 * 1024,
		WriteTimeout:      10 * time.Second,
	}

	err := s.ListenAndServe()
	slog.Error("Server failed", "error", err)
}
