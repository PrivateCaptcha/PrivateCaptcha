package main

import (
	"fmt"
	"net/http"
	"os"

	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

var (
	GitCommit string
)

func main() {
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	server := &api.Server{
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
	err := s.ListenAndServe()
	slog.Error("Server failed", "error", err)
}
