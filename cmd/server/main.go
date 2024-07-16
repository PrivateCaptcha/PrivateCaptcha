package main

import (
	"context"
	"flag"
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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session/store/memory"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

var (
	GitCommit string
	flagMode  = flag.String("mode", "", "migrate | run")
)

func run(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	stage := getenv("STAGE")
	common.SetupLogs(stage, getenv("VERBOSE") == "1")

	cache, cerr := db.NewMemoryCache(5 * time.Minute)
	if cerr != nil {
		return cerr
	}

	paddleAPI, err := billing.NewPaddleAPI(getenv)
	if err != nil {
		return err
	}

	ratelimiter, err := ratelimit.NewHTTPRateLimiter(getenv(common.ConfigRateLimitHeader))
	if err != nil {
		return err
	}

	pool, clickhouse, dberr := db.Connect(ctx, getenv)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	businessDB := db.NewBusiness(pool, cache, 5*time.Minute)
	timeSeriesDB := db.NewTimeSeries(clickhouse)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	apiServer := api.NewServer(businessDB, timeSeriesDB, 30*time.Second, paddleAPI, &email.StubAdminMailer{}, os.Getenv)

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
		Mailer:    &email.StubMailer{},
		PaddleAPI: paddleAPI,
	}

	router := http.NewServeMux()

	apiAuth := api.NewAuthMiddleware(os.Getenv, businessDB, ratelimiter, 1*time.Second)

	apiServer.Setup(router, "api", apiAuth)
	apiServer.StartMaintenanceJobs()

	portalServer.Init()
	portalServer.Setup(router, ratelimiter.RateLimit)
	router.Handle("GET /assets/", http.StripPrefix("/assets/", web.Static()))
	portalServer.StartMaintenanceJobs()

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
		sessionStore.Shutdown()
		apiServer.Shutdown()
		portalServer.Shutdown()
		businessDB.Shutdown()
		apiAuth.Shutdown()
		ratelimiter.Shutdown()
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

func migrate(ctx context.Context, getenv func(string) string) error {
	stage := getenv("STAGE")
	common.SetupLogs(stage, getenv("VERBOSE") == "1")

	ctx = context.WithValue(ctx, common.TraceIDContextKey, "migration")
	return db.Migrate(ctx, getenv)
}

func main() {
	flag.Parse()

	var err error

	switch *flagMode {
	case "run":
		err = run(context.Background(), os.Getenv, os.Stderr)
	case "migrate":
		err = migrate(context.Background(), os.Getenv)
	default:
		err = fmt.Errorf("unknown mode: '%s'", *flagMode)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
