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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
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
	GitCommit       string
	flagMode        = flag.String("mode", "", strings.Join([]string{modeMigrate, modeSystemd, modeServer}, " | "))
	envFileFlag     = flag.String("env", "", "Path to .env file")
	versionFlag     = flag.Bool("version", false, "Print version and exit")
	migrateHashFlag = flag.String("migrate-hash", "", "Target migration version (git commit)")
)

func run(ctx context.Context, cfg *config.Config, stderr io.Writer, systemdListener bool) error {
	common.SetupLogs(cfg.Stage(), cfg.Verbose())

	var cache common.Cache[string, any]
	var err error
	cache, err = db.NewMemoryCache[string, any](maxCacheSize, nil /*missing value*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create memory cache for server", common.ErrAttr(err))
		cache = db.NewStaticCache[string, any](maxCacheSize, nil /*missing value*/)
	}

	paddleAPI, err := billing.NewPaddleAPI(cfg.Getenv)
	if err != nil {
		return err
	}

	ratelimiter := ratelimit.NewIPAddrRateLimiter(cfg.RateLimiterHeader())

	pool, clickhouse, dberr := db.Connect(ctx, cfg)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	businessDB := db.NewBusiness(pool, cache)
	timeSeriesDB := db.NewTimeSeries(clickhouse)

	var userLimits common.Cache[int32, *common.UserLimitStatus]
	userLimits, err = db.NewMemoryCache[int32, *common.UserLimitStatus](maxLimitedUsers, nil /*missing value*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create memory cache for user limits", common.ErrAttr(err))
		userLimits = db.NewStaticCache[int32, *common.UserLimitStatus](maxLimitedUsers, nil /*missing data*/)
	}
	apiAuth := api.NewAuthMiddleware(cfg.Getenv, businessDB, ratelimiter, userLimits, 1*time.Second /*backfill duration*/)
	metrics := monitoring.NewService(cfg.Getenv)
	apiServer := api.NewServer(businessDB, timeSeriesDB, apiAuth, 30*time.Second /*flush interval*/, paddleAPI, metrics, cfg.Getenv)

	router := http.NewServeMux()

	apiServer.Setup(router, cfg.APIDomain(), cfg.Verbose())

	mailer := email.NewMailer(cfg.Getenv)
	portalMailer := email.NewPortalMailer("https:"+cfg.CDNURL(), cfg.PortalURL(), mailer, cfg.Getenv)

	sessionStore := db.NewSessionStore(pool, memory.New(), 1*time.Minute, session.KeyPersistent)
	portalServer := &portal.Server{
		Stage:      cfg.Stage(),
		Store:      businessDB,
		TimeSeries: timeSeriesDB,
		XSRF:       portal.XSRFMiddleware{Key: "pckey", Timeout: 1 * time.Hour},
		Session: session.Manager{
			CookieName:  "pcsid",
			Store:       sessionStore,
			MaxLifetime: sessionStore.MaxLifetime(),
		},
		PaddleAPI: paddleAPI,
		APIURL:    cfg.APIURL(),
		CDNURL:    cfg.CDNURL(),
		Verifier:  apiServer,
		Metrics:   metrics,
		Mailer:    portalMailer,
	}
	portalServer.Init()

	healthCheck := &maintenance.HealthCheckJob{
		BusinessDB:    businessDB,
		TimeSeriesDB:  timeSeriesDB,
		WithSystemd:   systemdListener,
		CheckInterval: cfg.HealthCheckInterval(),
		Router:        router,
	}

	portalDomain := cfg.PortalDomain()
	portalServer.Setup(router, portalDomain, apiAuth.EdgeVerify(portalDomain), ratelimiter.RateLimit)
	defaultAPIChain := alice.New(common.Recovered)
	router.Handle(http.MethodGet+" /"+common.HealthEndpoint, defaultAPIChain.Then(ratelimiter.RateLimit(http.HandlerFunc(healthCheck.HandlerFunc))))
	cdnDomain := cfg.CDNDomain()
	cdnChain := alice.New(common.Recovered, apiAuth.EdgeVerify(cdnDomain), ratelimiter.RateLimit)
	router.Handle("GET "+cdnDomain+"/portal/", http.StripPrefix("/portal/", cdnChain.Then(web.Static())))
	router.Handle("GET "+cdnDomain+"/widget/", http.StripPrefix("/widget/", cdnChain.Then(widget.Static())))

	httpServer := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1024 * 1024,
	}

	updateConfigFunc := func() {
		maintenanceMode := cfg.MaintenanceMode()
		businessDB.UpdateConfig(maintenanceMode)
		timeSeriesDB.UpdateConfig(maintenanceMode)
		portalServer.UpdateConfig(cfg)
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
		address := cfg.ListenAddress()
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
				if len(*envFileFlag) > 0 {
					_ = godotenv.Load(*envFileFlag)
				}
				updateConfigFunc()
			case syscall.SIGINT, syscall.SIGTERM:
				healthCheck.Shutdown(ctx)
				close(quit)
				return
			}
		}
	}()

	go func() {
		slog.InfoContext(ctx, "Listening", "address", listeners[0].Addr().String(), "version", GitCommit, "stage", cfg.Stage())
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
		Mailer: email.NewAdminMailer(mailer, cfg.Getenv),
		Store:  businessDB,
	})
	jobs.AddLocked(6*time.Hour, &maintenance.PaddlePricesJob{
		Stage:     cfg.Stage(),
		PaddleAPI: paddleAPI,
		Store:     businessDB,
	})
	jobs.Add(&maintenance.SessionsCleanupJob{
		Session: portalServer.Session,
	})
	// NOTE: this job should not be DB-locked as we need to have a blocklist on every server
	jobs.Add(&maintenance.ThrottleViolationsJob{
		Stage:      cfg.Stage(),
		UserLimits: userLimits,
		Store:      businessDB,
		From:       common.StartOfMonth(),
	})
	jobs.Add(&maintenance.CleanupDBCacheJob{Store: businessDB})
	jobs.Add(&maintenance.CleanupDeletedRecordsJob{Store: businessDB, Age: 365 * 24 * time.Hour})
	jobs.AddOneOff(&maintenance.WarmupPaddlePrices{
		Store: businessDB,
		Stage: cfg.Stage(),
	})
	jobs.AddLocked(24*time.Hour, &maintenance.GarbageCollectDataJob{
		Age:        30 * 24 * time.Hour,
		BusinessDB: businessDB,
		TimeSeries: timeSeriesDB,
	})
	jobs.AddOneOff(&maintenance.WarmupPortalAuth{
		Store: businessDB,
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

func migrate(ctx context.Context, cfg *config.Config, up bool) error {
	if len(*migrateHashFlag) == 0 {
		return errors.New("empty migrate hash")
	}

	if *migrateHashFlag != "ignore" && *migrateHashFlag != GitCommit {
		return fmt.Errorf("target version (%v) does not match built version (%v)", *migrateHashFlag, GitCommit)
	}

	common.SetupLogs(cfg.Stage(), cfg.Verbose())
	slog.InfoContext(ctx, "Migrating", "up", up, "version", GitCommit, "stage", cfg.Stage())

	return db.Migrate(ctx, cfg, up)
}

func main() {
	flag.Parse()

	if *versionFlag {
		fmt.Print(GitCommit)
		return
	}

	var err error

	if len(*envFileFlag) > 0 {
		err = godotenv.Load(*envFileFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
	}

	cfg, err := config.New(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	switch *flagMode {
	case modeServer, modeSystemd:
		ctx := common.TraceContext(context.Background(), "main")
		err = run(ctx, cfg, os.Stderr, (*flagMode == modeSystemd))
	case modeMigrate:
		ctx := common.TraceContext(context.Background(), "migration")
		err = migrate(ctx, cfg, true /*up*/)
	case modeRollback:
		ctx := common.TraceContext(context.Background(), "migration")
		err = migrate(ctx, cfg, false /*up*/)
	default:
		err = fmt.Errorf("unknown mode: '%s'", *flagMode)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
