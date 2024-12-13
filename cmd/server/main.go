package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session/store/memory"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/PrivateCaptcha/PrivateCaptcha/widget"
	"github.com/coreos/go-systemd/v22/activation"
	"github.com/joho/godotenv"
	"github.com/justinas/alice"
)

const (
	maxCacheSize    = 1_000_000
	maxLimitedUsers = 10_000
	modeMigrate     = "migrate"
	modeRollback    = "rollback"
	modeSystemd     = "systemd"
	modeServer      = "server"
)

var (
	GitCommit   string
	flagMode    = flag.String("mode", "", strings.Join([]string{modeMigrate, modeSystemd, modeServer}, " | "))
	envFileFlag = flag.String("env", "", "Path to .env file")
)

func run(ctx context.Context, getenv func(string) string, stderr io.Writer, systemdListener bool) error {
	stage := getenv("STAGE")
	verbose := getenv("VERBOSE") == "1"
	common.SetupLogs(stage, verbose)

	var cache common.Cache[string, any]
	var err error
	cache, err = db.NewMemoryCache[string, any](5*time.Minute, maxCacheSize, nil /*missing value*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create memory cache for server", common.ErrAttr(err))
		cache = db.NewStaticCache[string, any](maxCacheSize, nil /*missing value*/)
	}

	paddleAPI, err := billing.NewPaddleAPI(getenv)
	if err != nil {
		return err
	}

	ratelimiter := ratelimit.NewIPAddrRateLimiter(getenv(common.ConfigRateLimitHeader))

	pool, clickhouse, dberr := db.Connect(ctx, getenv)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	businessDB := db.NewBusiness(pool, cache)
	timeSeriesDB := db.NewTimeSeries(clickhouse)

	var userLimits common.Cache[int32, *common.UserLimitStatus]
	userLimits, err = db.NewMemoryCache[int32, *common.UserLimitStatus](3*time.Hour, maxLimitedUsers, nil /*missing value*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create memory cache for user limits", common.ErrAttr(err))
		userLimits = db.NewStaticCache[int32, *common.UserLimitStatus](maxLimitedUsers, nil /*missing data*/)
	}
	apiAuth := api.NewAuthMiddleware(getenv, businessDB, ratelimiter, userLimits, 1*time.Second /*backfill duration*/)
	metrics := monitoring.NewService(getenv)
	apiServer := api.NewServer(businessDB, timeSeriesDB, apiAuth, 30*time.Second /*flush interval*/, paddleAPI, metrics, getenv)

	host := getenv("PC_HOST")
	if host == "" {
		host = "localhost"
	}

	domain := getenv("PC_DOMAIN")
	if domain == "" {
		domain = host
	}

	router := http.NewServeMux()

	apiDomain := "api." + domain
	apiServer.Setup(router, apiDomain, "" /*prefix*/, verbose)

	sessionStore := db.NewSessionStore(pool, memory.New(), 1*time.Minute, session.KeyPersistent)
	portalServer := &portal.Server{
		Stage:      stage,
		Store:      businessDB,
		TimeSeries: timeSeriesDB,
		Domain:     "portal." + domain,
		XSRF:       portal.XSRFMiddleware{Key: "key", Timeout: 1 * time.Hour},
		Session: session.Manager{
			CookieName:  "pcsid",
			Store:       sessionStore,
			MaxLifetime: sessionStore.MaxLifetime(),
		},
		PaddleAPI: paddleAPI,
		ApiRelURL: "http://" + apiDomain,
		Verifier:  apiServer,
		Metrics:   metrics,
	}
	mailer := email.NewMailer(getenv)
	portalMailer := email.NewPortalMailer("http://"+portalServer.Domain, mailer, getenv)
	portalServer.Mailer = portalMailer
	portalServer.Init()

	healthCheck := &maintenance.HealthCheckJob{
		BusinessDB:    businessDB,
		TimeSeriesDB:  timeSeriesDB,
		WithSystemd:   systemdListener,
		CheckInterval: 5 * time.Second,
		Router:        router,
	}
	if "slow" == getenv("HEALTHCHECK") {
		healthCheck.CheckInterval = 1 * time.Minute
	}

	portalServer.Setup(router, ratelimiter.RateLimit)
	router.Handle("GET "+portalServer.Domain+"/assets/", http.StripPrefix("/assets/", ratelimiter.RateLimit(web.Static())))
	defaultAPIChain := alice.New(common.NoCache, common.Recovered)
	router.Handle(http.MethodGet+" /"+common.HealthEndpoint, defaultAPIChain.Then(ratelimiter.RateLimit(http.HandlerFunc(healthCheck.HandlerFunc))))
	router.Handle("GET "+portalServer.Domain+"/widget/", http.StripPrefix("/widget/", widget.Static()))

	httpServer := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 4 * time.Second,
		ReadTimeout:       10 * time.Second,
		MaxHeaderBytes:    256 * 1024,
		WriteTimeout:      10 * time.Second,
	}

	updateConfigFunc := func() {
		maintenanceMode := common.EnvToBool(getenv("PC_MAINTENANCE_MODE"))
		businessDB.UpdateConfig(maintenanceMode)
		timeSeriesDB.UpdateConfig(maintenanceMode)
		portalServer.UpdateConfig(maintenanceMode)
	}
	updateConfigFunc()

	var listeners []net.Listener
	if systemdListener {
		listeners, err = activation.Listeners()
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve systemd listeners", common.ErrAttr(err))
			return err
		}

		if len(listeners) != 1 {
			slog.ErrorContext(ctx, "Unexpected number of systemd listeners available", "count", len(listeners))
			return errors.New("unexpected number of systemd listeners")
		}
	} else {
		port := getenv("PC_PORT")
		if port == "" {
			port = "8080"
		}
		if stage != common.StageProd {
			// TODO: Fix this properly
			portalServer.ApiRelURL += ":" + port
			portalMailer.Domain += ":" + port
		}

		address := net.JoinHostPort(host, port)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to listen", "address", address, common.ErrAttr(err))
			return err
		}
		listeners = append(listeners, listener)
	}

	quit := make(chan struct{})
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		defer func() {
			signal.Stop(signals)
			close(signals)
		}()
		for {
			sig, ok := <-signals
			if !ok {
				slog.DebugContext(ctx, "Signals channel closed")
				return
			}
			slog.DebugContext(ctx, "Received signal", "signal", sig)
			switch sig {
			case syscall.SIGHUP:
				updateConfigFunc()
			case syscall.SIGINT, syscall.SIGTERM:
				healthCheck.Shutdown(ctx)
				close(quit)
				return
			}
		}
	}()

	go func() {
		slog.InfoContext(ctx, "Listening", "address", listeners[0].Addr().String(), "version", GitCommit, "stage", stage)
		if err := httpServer.Serve(listeners[0]); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(ctx, "Error serving", common.ErrAttr(err))
		}
	}()

	// start maintenance jobs
	jobs := maintenance.NewJobs(businessDB)
	jobs.Add(healthCheck)
	jobs.AddLocked(4*time.Hour, &maintenance.UsageLimitsJob{
		MaxUsers:   200, // it will be a truly great problem to have when 200 will be not enough
		From:       common.StartOfMonth(),
		BusinessDB: businessDB,
		TimeSeries: timeSeriesDB,
	})
	jobs.AddLocked(24*time.Hour, &maintenance.NotifyLimitsViolationsJob{
		Mailer: email.NewAdminMailer(mailer, getenv),
		Store:  businessDB,
	})
	jobs.AddLocked(6*time.Hour, &maintenance.PaddlePricesJob{
		Stage:     stage,
		PaddleAPI: paddleAPI,
		Store:     businessDB,
	})
	jobs.Add(&maintenance.SessionsCleanupJob{
		Session: portalServer.Session,
	})
	// NOTE: this job should not be DB-locked as we need to have a blocklist on every server
	jobs.Add(&maintenance.ThrottleViolationsJob{
		Stage:      stage,
		UserLimits: userLimits,
		Store:      businessDB,
		From:       common.StartOfMonth(),
	})
	jobs.Add(&maintenance.CleanupDBCacheJob{Store: businessDB})
	jobs.Add(&maintenance.CleanupDeletedRecordsJob{Store: businessDB, Age: 365 * 24 * time.Hour})
	jobs.AddOneOff(&maintenance.WarmupPaddlePrices{
		Store: businessDB,
		Stage: stage,
	})
	jobs.AddLocked(24*time.Hour, &maintenance.GarbageCollectDataJob{
		Age:        30 * 24 * time.Hour,
		BusinessDB: businessDB,
		TimeSeries: timeSeriesDB,
	})
	jobs.Run()

	metrics.StartServing(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-quit
		slog.DebugContext(ctx, "Shutting down gracefully")
		jobs.Shutdown()
		sessionStore.Shutdown()
		apiServer.Shutdown()
		apiAuth.Shutdown()
		ratelimiter.Shutdown()
		metrics.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.SetKeepAlivesEnabled(false)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "error shutting down http server: %s\n", err)
		}
		slog.DebugContext(ctx, "Shutdown finished")
	}()

	wg.Wait()
	return nil
}

func migrate(ctx context.Context, getenv func(string) string, up bool) error {
	stage := getenv("STAGE")
	common.SetupLogs(stage, getenv("VERBOSE") == "1")

	ctx = context.WithValue(ctx, common.TraceIDContextKey, "migration")
	return db.Migrate(ctx, getenv, up)
}

func main() {
	flag.Parse()

	var err error

	if len(*envFileFlag) > 0 {
		err = godotenv.Load(*envFileFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
	}

	switch *flagMode {
	case modeServer, modeSystemd:
		err = run(common.TraceContext(context.Background(), "main"), os.Getenv, os.Stderr, (*flagMode == modeSystemd))
	case modeMigrate:
		err = migrate(context.Background(), os.Getenv, true /*up*/)
	case modeRollback:
		err = migrate(context.Background(), os.Getenv, false /*up*/)
	default:
		err = fmt.Errorf("unknown mode: '%s'", *flagMode)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
