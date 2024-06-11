package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session/store/memory"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

var (
	GitCommit string
)

const (
	propertyBucketSize = 5 * time.Minute
	levelsBatchSize    = 100
)

func run(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	stage := getenv("STAGE")
	common.SetupLogs(stage, getenv("VERBOSE") == "1")

	cache, cerr := db.NewMemoryCache(5 * time.Minute)
	if cerr != nil {
		panic(cerr)
	}

	paddleAPI, err := billing.NewPaddleAPI(getenv)
	if err != nil {
		panic(err)
	}

	pool, clickhouse, dberr := db.Migrate(getenv)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	businessDB := db.NewBusiness(pool, cache, 5*time.Minute)
	timeSeriesDB := db.NewTimeSeries(clickhouse)

	levels := difficulty.NewLevels(timeSeriesDB, levelsBatchSize, propertyBucketSize)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	apiServer := api.NewServer(businessDB, timeSeriesDB, levels, 30*time.Second, paddleAPI, os.Getenv)

	sessionStore := db.NewSessionStore(pool, memory.New(), 1*time.Minute, session.KeyPersistent)

	portalServer := &portal.Server{
		Stage:      stage,
		Store:      businessDB,
		TimeSeries: timeSeriesDB,
		Prefix:     "portal",
		XSRF:       portal.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
		Session: session.Manager{
			CookieName:  "pcsid",
			Store:       sessionStore,
			MaxLifetime: sessionStore.MaxLifetime(),
		},
		Mailer: &portal.StubMailer{},
	}

	go portalServer.Session.GC()

	router := http.NewServeMux()

	apiAuth := api.NewAuthMiddleware(os.Getenv, businessDB, 1*time.Second)

	apiServer.Setup(router, "api", apiAuth)
	portalServer.Init()
	portalServer.Setup(router)
	router.Handle("GET /assets/", http.StripPrefix("/assets/", web.Static()))

	host := getenv("PC_HOST")
	if host == "" {
		host = "localhost"
	}

	port := getenv("PC_PORT")
	if port == "" {
		port = "8080"
	}

	httpServer := &http.Server{
		Addr:              net.JoinHostPort(host, port),
		Handler:           router,
		ReadHeaderTimeout: 4 * time.Second,
		ReadTimeout:       10 * time.Second,
		MaxHeaderBytes:    256 * 1024,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		slog.Info("Listening", "address", httpServer.Addr, "version", GitCommit)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Error listening and serving", common.ErrAttr(err))
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		slog.Debug("Shutting down gracefully...")
		levels.Shutdown()
		sessionStore.Shutdown()
		apiServer.Shutdown()
		businessDB.Shutdown()
		apiAuth.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "error shutting down http server: %s\n", err)
		}
		slog.Debug("Shutdown finished")
	}()

	wg.Wait()
	return nil
}

func main() {
	if err := run(context.Background(), os.Getenv, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
