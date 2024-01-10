package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/redis/go-redis/v9"
)

var (
	GitCommit string
)

func main() {
	stage := os.Getenv("STAGE")
	common.SetupLogs(stage, os.Getenv("VERBOSE") == "1")

	pool, err := db.Connect(context.TODO(), os.Getenv("PC_POSTGRES"))
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	store := &db.Store{
		Queries: dbgen.New(pool),
	}

	opts, err := redis.ParseURL(os.Getenv("PC_REDIS"))
	if err != nil {
		panic(err)
	}
	rdb := redis.NewClient(opts)

	cache := &db.Cache{
		Redis: rdb,
	}

	server := &api.Server{
		Auth: &api.AuthMiddleware{
			Store: store,
			Cache: cache,
		},
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

	slog.Info("Starting", "address", fmt.Sprintf("http://%v:%v", host, port), "version", GitCommit)

	s := &http.Server{
		Addr:    host + ":" + port,
		Handler: router,
	}
	err = s.ListenAndServe()
	slog.Error("Server failed", "error", err)
}
