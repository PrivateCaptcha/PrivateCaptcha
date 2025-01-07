package main

import (
	"context"
	"crypto/tls"
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
	certFileFlag    = flag.String("certfile", "", "certificate PEM file (e.g. cert.pem)")
	keyFileFlag     = flag.String("keyfile", "", "key PEM file (e.g. key.pem)")
)

func createListener(ctx context.Context, cfg *config.Config) (net.Listener, bool, error) {
	var listener net.Listener
	systemdListener := (*flagMode == modeSystemd)
	if systemdListener {
		listeners, err := activation.Listeners()
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve systemd listeners", common.ErrAttr(err))
			return nil, systemdListener, err
		}

		if len(listeners) != 1 {
			slog.ErrorContext(ctx, "Unexpected number of systemd listeners available", "count", len(listeners))
			return nil, systemdListener, errors.New("unexpected number of systemd listeners")
		}

		listener = listeners[0]
	} else {
		address := cfg.ListenAddress()
		var err error
		listener, err = net.Listen("tcp", address)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to listen", "address", address, common.ErrAttr(err))
			return nil, systemdListener, err
		}
	}

	if useTLS := (*certFileFlag != "") && (*keyFileFlag != ""); useTLS {
		cert, err := tls.LoadX509KeyPair(*certFileFlag, *keyFileFlag)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to load certificates", "cert", *certFileFlag, "key", *keyFileFlag, common.ErrAttr(err))
			return nil, systemdListener, err
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		listener = tls.NewListener(listener, tlsConfig)
	}

	return listener, systemdListener, nil
}

func run(ctx context.Context, cfg *config.Config, stderr io.Writer, listener net.Listener, systemdListener bool) error {
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
	auth := api.NewAuthMiddleware(cfg, businessDB, userLimits, 1*time.Second /*backfill duration*/)
	metrics := monitoring.NewService(cfg.Getenv)

	mailer := email.NewMailer(cfg.Getenv)
	portalMailer := email.NewPortalMailer("https:"+cfg.CDNURL(), cfg.PortalURL(), mailer, cfg.Getenv)

	apiServer := api.NewServer(businessDB, timeSeriesDB, auth, 30*time.Second /*flush interval*/, paddleAPI, metrics, portalMailer, cfg.Getenv)

	router := http.NewServeMux()

	apiServer.Setup(router, cfg.APIDomain(), cfg.Verbose())

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
		PaddleAPI:   paddleAPI,
		APIURL:      cfg.APIURL(),
		CDNURL:      cfg.CDNURL(),
		Verifier:    apiServer,
		Metrics:     metrics,
		Mailer:      portalMailer,
		RateLimiter: auth.DefaultRateLimiter(),
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
	portalServer.Setup(router, portalDomain, auth.EdgeVerify(portalDomain))
	rateLimiter := auth.DefaultRateLimiter().RateLimit
	cdnDomain := cfg.CDNDomain()
	cdnChain := alice.New(common.Recovered, auth.EdgeVerify(cdnDomain), metrics.Handler, rateLimiter)
	router.Handle("GET "+cdnDomain+"/portal/", http.StripPrefix("/portal/", cdnChain.Then(web.Static())))
	router.Handle("GET "+cdnDomain+"/widget/", http.StripPrefix("/widget/", cdnChain.Then(widget.Static())))
	internalAPIChain := alice.New(common.Recovered, rateLimiter, common.NoCache)
	router.Handle(http.MethodGet+" /"+common.HealthEndpoint, internalAPIChain.ThenFunc(healthCheck.HandlerFunc))
	// "protection" (NOTE: different than usual order of monitoring)
	publicChain := alice.New(common.Recovered, auth.EdgeVerify("" /*domain*/), metrics.Handler, rateLimiter)
	common.SetupWellKnownPaths(router, publicChain)

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
		auth.UpdateConfig(cfg)
	}
	updateConfigFunc()

	quit := make(chan struct{})
	go func(ctx context.Context) {
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
	}(common.TraceContext(context.Background(), "signal_handler"))

	go func() {
		slog.InfoContext(ctx, "Listening", "address", listener.Addr().String(), "version", GitCommit, "stage", cfg.Stage())
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
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

	var localServer *http.Server
	if localAddress := cfg.LocalAddress(); len(localAddress) > 0 {
		localRouter := http.NewServeMux()
		metrics.Setup(localRouter)
		jobs.Setup(localRouter)
		localServer = &http.Server{
			Addr:    localAddress,
			Handler: localRouter,
		}
		go func() {
			slog.InfoContext(ctx, "Serving local API", "address", localServer.Addr)
			if err := localServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.ErrorContext(ctx, "Error serving local API", common.ErrAttr(err))
			}
		}()
	} else {
		slog.DebugContext(ctx, "Skipping serving local API")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-quit
		slog.DebugContext(ctx, "Shutting down gracefully")
		jobs.Shutdown()
		sessionStore.Shutdown()
		apiServer.Shutdown()
		auth.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.SetKeepAlivesEnabled(false)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "error shutting down http server: %s\n", err)
		}
		if localServer != nil {
			localServer.Close()
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
		if listener, systemdListener, lerr := createListener(ctx, cfg); lerr == nil {
			err = run(ctx, cfg, os.Stderr, listener, systemdListener)
		} else {
			err = lerr
		}
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
