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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session/store/memory"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/PrivateCaptcha/PrivateCaptcha/widget"
	"github.com/justinas/alice"
)

const (
	modeMigrate          = "migrate"
	modeRollback         = "rollback"
	modeServer           = "server"
	_readinessDrainDelay = 1 * time.Second
	_shutdownHardPeriod  = 3 * time.Second
	_shutdownPeriod      = 10 * time.Second
	_dbConnectTimeout    = 30 * time.Second
)

var (
	GitCommit       string
	flagMode        = flag.String("mode", "", strings.Join([]string{modeMigrate, modeServer}, " | "))
	envFileFlag     = flag.String("env", "", "Path to .env file, 'stdin' or empty")
	versionFlag     = flag.Bool("version", false, "Print version and exit")
	migrateHashFlag = flag.String("migrate-hash", "", "Target migration version (git commit)")
	certFileFlag    = flag.String("certfile", "", "certificate PEM file (e.g. cert.pem)")
	keyFileFlag     = flag.String("keyfile", "", "key PEM file (e.g. key.pem)")
	env             *common.EnvMap
)

func listenAddress(cfg common.ConfigStore) string {
	host := cfg.Get(common.HostKey).Value()
	if host == "" {
		host = "localhost"
	}

	port := cfg.Get(common.PortKey).Value()
	if port == "" {
		port = "8080"
	}
	address := net.JoinHostPort(host, port)
	return address
}

func createListener(ctx context.Context, cfg common.ConfigStore) (net.Listener, error) {
	address := listenAddress(cfg)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to listen", "address", address, common.ErrAttr(err))
		return nil, err
	}

	if useTLS := (*certFileFlag != "") && (*keyFileFlag != ""); useTLS {
		cert, err := tls.LoadX509KeyPair(*certFileFlag, *keyFileFlag)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to load certificates", "cert", *certFileFlag, "key", *keyFileFlag, common.ErrAttr(err))
			return nil, err
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		listener = tls.NewListener(listener, tlsConfig)
	}

	return listener, nil
}

func run(ctx context.Context, cfg common.ConfigStore, stderr io.Writer, listener net.Listener) error {
	stage := cfg.Get(common.StageKey).Value()
	verbose := config.AsBool(cfg.Get(common.VerboseKey))
	common.SetupLogs(stage, verbose)

	planService := billing.NewPlanService(nil)

	pool, clickhouse, dberr := db.Connect(ctx, cfg, _dbConnectTimeout, false /*admin*/)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	businessDB := db.NewBusiness(pool)
	timeSeriesDB := db.NewTimeSeries(clickhouse)

	metrics := monitoring.NewService()

	cdnURLConfig := config.AsURL(ctx, cfg.Get(common.CDNBaseURLKey))
	portalURLConfig := config.AsURL(ctx, cfg.Get(common.PortalBaseURLKey))

	mailer := email.NewMailer(cfg)
	portalMailer := email.NewPortalMailer("https:"+cdnURLConfig.URL(), portalURLConfig.Domain(), mailer, cfg)

	apiServer := &api.Server{
		Stage:              stage,
		BusinessDB:         businessDB,
		TimeSeries:         timeSeriesDB,
		Auth:               api.NewAuthMiddleware(cfg, businessDB, api.NewUserLimiter(businessDB), planService),
		VerifyLogChan:      make(chan *common.VerifyRecord, 10*api.VerifyBatchSize),
		Salt:               api.NewPuzzleSalt(cfg.Get(common.APISaltKey)),
		UserFingerprintKey: api.NewUserFingerprintKey(cfg.Get(common.UserFingerprintIVKey)),
		Metrics:            metrics,
		Mailer:             portalMailer,
		Levels:             difficulty.NewLevels(timeSeriesDB, 100 /*levelsBatchSize*/, api.PropertyBucketSize),
		VerifyLogCancel:    func() {},
	}
	if err := apiServer.Init(ctx, 10*time.Second /*flush interval*/, 1*time.Second /*backfill duration*/); err != nil {
		return err
	}

	router := http.NewServeMux()

	apiURLConfig := config.AsURL(ctx, cfg.Get(common.APIBaseURLKey))
	apiDomain := apiURLConfig.Domain()
	apiServer.Setup(router, apiDomain, verbose, common.NoopMiddleware)

	sessionStore := db.NewSessionStore(pool, memory.New(), 1*time.Minute, session.KeyPersistent)
	portalServer := &portal.Server{
		Stage:      stage,
		Store:      businessDB,
		TimeSeries: timeSeriesDB,
		XSRF:       &common.XSRFMiddleware{Key: "pckey", Timeout: 1 * time.Hour},
		Sessions: &session.Manager{
			CookieName:  "pcsid",
			Store:       sessionStore,
			MaxLifetime: sessionStore.MaxLifetime(),
		},
		PlanService:  planService,
		APIURL:       apiURLConfig.URL(),
		CDNURL:       cdnURLConfig.URL(),
		PuzzleEngine: apiServer,
		Metrics:      metrics,
		Mailer:       portalMailer,
		Auth:         portal.NewAuthMiddleware(portal.NewRateLimiter(cfg)),
	}

	templatesBuilder := portal.NewTemplatesBuilder()
	if err := templatesBuilder.AddFS(ctx, web.Templates(), "core"); err != nil {
		return err
	}

	if err := portalServer.Init(ctx, templatesBuilder); err != nil {
		return err
	}

	healthCheck := &maintenance.HealthCheckJob{
		BusinessDB:    businessDB,
		TimeSeriesDB:  timeSeriesDB,
		CheckInterval: cfg.Get(common.HealthCheckIntervalKey),
		Metrics:       metrics,
	}

	portalDomain := portalURLConfig.Domain()
	_ = portalServer.Setup(router, portalDomain, common.NoopMiddleware)
	rateLimiter := portalServer.Auth.RateLimit()
	cdnDomain := cdnURLConfig.Domain()
	cdnChain := alice.New(common.Recovered, metrics.CDNHandler, rateLimiter)
	router.Handle("GET "+cdnDomain+"/portal/", http.StripPrefix("/portal/", cdnChain.Then(web.Static())))
	router.Handle("GET "+cdnDomain+"/widget/", http.StripPrefix("/widget/", cdnChain.Then(widget.Static())))
	// "protection" (NOTE: different than usual order of monitoring)
	publicChain := alice.New(common.Recovered, metrics.IgnoredHandler, rateLimiter)
	portalServer.SetupCatchAll(router, portalDomain, publicChain)
	common.SetupWellKnownPaths(router, publicChain)

	ongoingCtx, stopOngoingGracefully := context.WithCancel(context.Background())
	httpServer := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1024 * 1024,
		BaseContext: func(_ net.Listener) context.Context {
			return ongoingCtx
		},
	}

	updateConfigFunc := func(ctx context.Context) {
		cfg.Update(ctx)
		maintenanceMode := config.AsBool(cfg.Get(common.MaintenanceModeKey))
		businessDB.UpdateConfig(maintenanceMode)
		timeSeriesDB.UpdateConfig(maintenanceMode)
		portalServer.UpdateConfig(ctx, cfg)
		apiServer.UpdateConfig(ctx, cfg)
	}
	updateConfigFunc(ctx)

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
				if uerr := env.Update(); uerr != nil {
					slog.ErrorContext(ctx, "Failed to update environment", common.ErrAttr(uerr))
				}
				updateConfigFunc(ctx)
			case syscall.SIGINT, syscall.SIGTERM:
				healthCheck.Shutdown(ctx)
				// Give time for readiness check to propagate
				time.Sleep(min(_readinessDrainDelay, healthCheck.Interval()))
				close(quit)
				return
			}
		}
	}(common.TraceContext(context.Background(), "signal_handler"))

	go func() {
		slog.InfoContext(ctx, "Listening", "address", listener.Addr().String(), "version", GitCommit, "stage", stage)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(ctx, "Error serving", common.ErrAttr(err))
		}
	}()

	// start maintenance jobs
	jobs := maintenance.NewJobs(businessDB)
	jobs.Add(healthCheck)
	jobs.Add(&maintenance.SessionsCleanupJob{
		Session: portalServer.Sessions,
	})
	jobs.Add(&maintenance.CleanupDBCacheJob{Store: businessDB})
	jobs.Add(&maintenance.CleanupDeletedRecordsJob{Store: businessDB, Age: 365 * 24 * time.Hour})
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
	if localAddress := cfg.Get(common.LocalAddressKey).Value(); len(localAddress) > 0 {
		localRouter := http.NewServeMux()
		metrics.Setup(localRouter)
		jobs.Setup(localRouter)
		localRouter.Handle(http.MethodGet+" /"+common.LiveEndpoint, common.Recovered(http.HandlerFunc(healthCheck.LiveHandler)))
		localRouter.Handle(http.MethodGet+" /"+common.ReadyEndpoint, common.Recovered(http.HandlerFunc(healthCheck.ReadyHandler)))
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
		portalServer.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), _shutdownPeriod)
		defer cancel()
		httpServer.SetKeepAlivesEnabled(false)
		serr := httpServer.Shutdown(shutdownCtx)
		stopOngoingGracefully()
		if serr != nil {
			slog.ErrorContext(ctx, "Failed to shutdown gracefully", common.ErrAttr(serr))
			fmt.Fprintf(stderr, "error shutting down http server gracefully: %s\n", serr)
			time.Sleep(_shutdownHardPeriod)
		}
		if localServer != nil {
			localServer.Close()
		}
		slog.DebugContext(ctx, "Shutdown finished")
	}()

	wg.Wait()
	return nil
}

func migrate(ctx context.Context, cfg common.ConfigStore, up bool) error {
	if len(*migrateHashFlag) == 0 {
		return errors.New("empty migrate hash")
	}

	if *migrateHashFlag != "ignore" && *migrateHashFlag != GitCommit {
		return fmt.Errorf("target version (%v) does not match built version (%v)", *migrateHashFlag, GitCommit)
	}

	stage := cfg.Get(common.StageKey).Value()
	verbose := config.AsBool(cfg.Get(common.VerboseKey))

	common.SetupLogs(stage, verbose)
	slog.InfoContext(ctx, "Migrating", "up", up, "version", GitCommit, "stage", stage)

	planService := billing.NewPlanService(nil)

	pool, clickhouse, dberr := db.Connect(ctx, cfg, _dbConnectTimeout, true /*admin*/)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	if err := db.MigratePostgres(ctx, pool, cfg, planService, up); err != nil {
		return err
	}

	if err := db.MigrateClickHouse(ctx, clickhouse, cfg, up); err != nil {
		return err
	}

	return nil
}

func main() {
	flag.Parse()

	if *versionFlag {
		fmt.Print(GitCommit)
		return
	}

	var err error
	env, err = common.NewEnvMap(*envFileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	cfg := config.NewEnvConfig(config.DefaultMapper, env.Get)

	if err = checkLicense(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	switch *flagMode {
	case modeServer:
		ctx := common.TraceContext(context.Background(), "main")
		if listener, lerr := createListener(ctx, cfg); lerr == nil {
			err = run(ctx, cfg, os.Stderr, listener)
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
