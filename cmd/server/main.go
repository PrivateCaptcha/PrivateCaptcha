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
)

var (
	GitCommit string
)

func main() {
	stage := os.Getenv("STAGE")
	common.SetupLogs(stage, os.Getenv("VERBOSE") == "1")

	pool, err := db.Connect(context.TODO(), os.Getenv("PC_POSTGRES"))
	if err != nil {
		os.Exit(1)
	}
	defer pool.Close()

	store := &db.Store{
		Queries: dbgen.New(pool),
	}

	server := &api.Server{
		Auth: &api.AuthMiddleware{
			Store: store,
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
