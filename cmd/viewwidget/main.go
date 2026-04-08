package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/PrivateCaptcha/PrivateCaptcha/widget"
	"github.com/rs/cors"
)

func main() {
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	srv := &server{salt: puzzle.NewSalt([]byte("salt"))}
	router := http.NewServeMux()

	corsDefault := cors.Default()

	router.Handle("/", monitoring.Logged(corsDefault.Handler(staticHandler())))
	router.Handle("GET "+"/widget/", http.StripPrefix("/widget/", widget.Static("")))
	router.Handle("GET "+"/assets/", http.StripPrefix("/assets/", web.Static("")))
	srv.Setup(router)

	host := os.Getenv("HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slog.Info("Starting", "address", fmt.Sprintf("http://%v:%v", host, port))

	s := &http.Server{
		Addr:    net.JoinHostPort(host, port),
		Handler: router,
	}
	err := s.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed", "error", err)
	}
}
