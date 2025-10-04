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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
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

const (
	// for puzzles the logic is that if something becomes popular, there will be a spike, but normal usage should be "low"
	// NOTE: this assumes correct configuration of the whole chain of reverse proxies
	// the main problem are NATs/VPNs that make possible for clump of legitimate users to actually come from 1 public IP
	generalLeakyBucketCap = 20
	generalLeakInterval   = 1 * time.Second
	// public defaults are reasonably low but we assume we should be fully cached on CDN level
	publicLeakyBucketCap = 8
	publicLeakInterval   = 2 * time.Second
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

func newIPAddrBuckets(cfg common.ConfigStore) *ratelimit.IPAddrBuckets {
	const (
		// number of simultaneous different clients for public APIs (/puzzle, /siteverify etc.), before forcing cleanup
		maxBuckets = 1_000_000
	)

	puzzleBucketRate := cfg.Get(common.RateLimitRateKey)
	puzzleBucketBurst := cfg.Get(common.RateLimitBurstKey)

	return ratelimit.NewIPAddrBuckets(maxBuckets,
		leakybucket.Cap(puzzleBucketBurst.Value(), generalLeakyBucketCap),
		leakybucket.Interval(puzzleBucketRate.Value(), generalLeakInterval))
}

func updateIPBuckets(cfg common.ConfigStore, rateLimiter ratelimit.HTTPRateLimiter) {
	bucketRate := cfg.Get(common.RateLimitRateKey)
	bucketBurst := cfg.Get(common.RateLimitBurstKey)
	rateLimiter.UpdateLimits(
		leakybucket.Cap(bucketBurst.Value(), generalLeakyBucketCap),
		leakybucket.Interval(bucketRate.Value(), generalLeakInterval))
}

func run(ctx context.Context, cfg common.ConfigStore, stderr io.Writer, listener net.Listener) error {
	stage := cfg.Get(common.StageKey).Value()
	verbose := config.AsBool(cfg.Get(common.VerboseKey))
	logLevel := common.SetupLogs(stage, verbose)

	planService := billing.NewPlanService(nil)

	pool, clickhouse, dberr := db.Connect(ctx, cfg, _dbConnectTimeout, false /*admin*/)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	businessDB := db.NewBusiness(pool)
	timeSeriesDB := db.NewTimeSeries(clickhouse)

	puzzleVerifier := api.NewVerifier(cfg, businessDB)

	metrics := monitoring.NewService()

	cdnURLConfig := config.AsURL(ctx, cfg.Get(common.CDNBaseURLKey))
	portalURLConfig := config.AsURL(ctx, cfg.Get(common.PortalBaseURLKey))

	sender := email.NewMailSender(cfg)
	mailer := portal.NewPortalMailer("https:"+cdnURLConfig.URL(), "https:"+portalURLConfig.URL(), sender, cfg)

	rateLimitHeader := cfg.Get(common.RateLimitHeaderKey).Value()
	ipRateLimiter := ratelimit.NewIPAddrRateLimiter(rateLimitHeader, newIPAddrBuckets(cfg))

	apiServer := &api.Server{
		Stage:           stage,
		BusinessDB:      businessDB,
		TimeSeries:      timeSeriesDB,
		RateLimiter:     ipRateLimiter,
		Auth:            api.NewAuthMiddleware(businessDB, api.NewUserLimiter(businessDB), planService),
		VerifyLogChan:   make(chan *common.VerifyRecord, 10*api.VerifyBatchSize),
		Verifier:        puzzleVerifier,
		Metrics:         metrics,
		Mailer:          mailer,
		Levels:          difficulty.NewLevels(timeSeriesDB, 100 /*levelsBatchSize*/, api.PropertyBucketSize),
		VerifyLogCancel: func() {},
	}
	if err := apiServer.Init(ctx, 10*time.Second /*flush interval*/, 1*time.Second /*backfill duration*/); err != nil {
		return err
	}

	dataCtx, err := web.LoadData()
	if err != nil {
		return err
	}

	apiURLConfig := config.AsURL(ctx, cfg.Get(common.APIBaseURLKey))
	sessionStore := db.NewSessionStore(businessDB, session.KeyPersistent)
	xsrfKey := cfg.Get(common.XSRFKeyKey)
	portalServer := &portal.Server{
		Stage:      stage,
		Store:      businessDB,
		TimeSeries: timeSeriesDB,
		XSRF:       &common.XSRFMiddleware{Key: xsrfKey.Value(), Timeout: 1 * time.Hour},
		Sessions: &session.Manager{
			CookieName:   "pcsid",
			Store:        sessionStore,
			MaxLifetime:  sessionStore.TTL(),
			SecureCookie: (*certFileFlag != "") && (*keyFileFlag != ""),
		},
		PlanService:  planService,
		APIURL:       apiURLConfig.URL(),
		CDNURL:       cdnURLConfig.URL(),
		PuzzleEngine: puzzleVerifier,
		Metrics:      metrics,
		Mailer:       mailer,
		RateLimiter:  ipRateLimiter,
		DataCtx:      dataCtx,
	}

	templatesBuilder := portal.NewTemplatesBuilder()
	if err := templatesBuilder.AddFS(ctx, web.Templates(), "core"); err != nil {
		return err
	}

	if err := portalServer.Init(ctx, templatesBuilder, GitCommit); err != nil {
		return err
	}

	healthCheck := &maintenance.HealthCheckJob{
		BusinessDB:    businessDB,
		TimeSeriesDB:  timeSeriesDB,
		CheckInterval: cfg.Get(common.HealthCheckIntervalKey),
		Metrics:       metrics,
	}
	jobs := maintenance.NewJobs(businessDB)

	updateConfigFunc := func(ctx context.Context) {
		cfg.Update(ctx)
		updateIPBuckets(cfg, ipRateLimiter)
		maintenanceMode := config.AsBool(cfg.Get(common.MaintenanceModeKey))
		businessDB.UpdateConfig(maintenanceMode)
		timeSeriesDB.UpdateConfig(maintenanceMode)
		portalServer.UpdateConfig(ctx, cfg)
		jobs.UpdateConfig(cfg)
		verboseLogs := config.AsBool(cfg.Get(common.VerboseKey))
		common.SetLogLevel(logLevel, verboseLogs)
	}
	updateConfigFunc(ctx)

	quit := make(chan struct{})
	quitFunc := func(ctx context.Context) {
		slog.DebugContext(ctx, "Server quit triggered")
		healthCheck.Shutdown(ctx)
		// Give time for readiness check to propagate
		time.Sleep(min(_readinessDrainDelay, healthCheck.Interval()))
		close(quit)
	}

	checkLicenseJob, err := maintenance.NewCheckLicenseJob(businessDB, cfg, GitCommit, quitFunc)
	if err != nil {
		return err
	}
	// nolint:errcheck
	go common.RunPeriodicJobOnce(common.TraceContext(context.Background(), "check_license"), checkLicenseJob, checkLicenseJob.NewParams())

	router := http.NewServeMux()
	apiServer.Setup(router, apiURLConfig.Domain(), verbose, common.NoopMiddleware)
	portalDomain := portalURLConfig.Domain()
	_ = portalServer.Setup(router, portalDomain, common.NoopMiddleware)
	rateLimiter := ipRateLimiter.RateLimitExFunc(publicLeakyBucketCap, publicLeakInterval)
	cdnDomain := cdnURLConfig.Domain()
	cdnChain := alice.New(common.Recovered, metrics.CDNHandler, rateLimiter)
	router.Handle("GET "+cdnDomain+"/portal/", http.StripPrefix("/portal/", cdnChain.Then(web.Static(GitCommit))))
	router.Handle("GET "+cdnDomain+"/widget/", http.StripPrefix("/widget/", cdnChain.Then(widget.Static(GitCommit))))
	// "protection" (NOTE: different than usual order of monitoring)
	publicChain := alice.New(common.Recovered, metrics.IgnoredHandler, rateLimiter)
	portalServer.SetupCatchAll(router, portalDomain, publicChain)
	router.Handle("/", publicChain.ThenFunc(common.CatchAll))

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
				quitFunc(ctx)
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

	jobs.Spawn(healthCheck)
	// start maintenance jobs
	jobs.Add(&maintenance.CleanupDBCacheJob{Store: businessDB})
	jobs.Add(&maintenance.CleanupDeletedRecordsJob{Store: businessDB, Age: 365 * 24 * time.Hour})
	jobs.AddLocked(24*time.Hour, &maintenance.GarbageCollectDataJob{
		Age:        30 * 24 * time.Hour,
		BusinessDB: businessDB,
		TimeSeries: timeSeriesDB,
	})
	jobs.AddOneOff(&maintenance.WarmupPortalAuthJob{
		Store:               businessDB,
		RegistrationAllowed: config.AsBool(cfg.Get(common.RegistrationAllowedKey)),
	})
	jobs.AddOneOff(&maintenance.WarmupAPICacheJob{
		Store:      businessDB,
		TimeSeries: timeSeriesDB,
		Backoff:    200 * time.Millisecond,
		Limit:      50,
	})
	jobs.AddLocked(2*time.Hour, checkLicenseJob)
	jobs.AddOneOff(&maintenance.RegisterEmailTemplatesJob{
		Templates: email.Templates(),
		Store:     businessDB,
	})
	jobs.AddLocked(1*time.Hour, &maintenance.UserEmailNotificationsJob{
		RunInterval:  3 * time.Hour, // overlap few locked intervals to cover for possible unprocessed notifications
		Store:        businessDB,
		Templates:    email.Templates(),
		Sender:       sender,
		ChunkSize:    50,
		MaxAttempts:  5,
		EmailFrom:    cfg.Get(common.EmailFromKey),
		ReplyToEmail: cfg.Get(common.ReplyToEmailKey),
		PlanService:  planService,
		CDNURL:       mailer.CDNURL,
		PortalURL:    mailer.PortalURL,
	})
	jobs.AddLocked(24*time.Hour, &maintenance.CleanupUserNotificationsJob{
		Store:              businessDB,
		NotificationMonths: 6,
		TemplateMonths:     7,
	})
	jobs.AddLocked(24*time.Hour, &maintenance.CleanupExpiredTrialUsersJob{
		Age:         30 * 24 * time.Hour,
		BusinessDB:  businessDB,
		PlanService: planService,
		ChunkSize:   20,
		Months:      6,
	})
	jobs.AddLocked(3*time.Hour, &maintenance.ExpireInternalTrialsJob{
		PastInterval: 3 * time.Hour,
		Age:          24 * time.Hour,
		BusinessDB:   businessDB,
		PlanService:  planService,
	})
	jobs.RunAll()

	var localServer *http.Server
	if localAddress := cfg.Get(common.LocalAddressKey).Value(); len(localAddress) > 0 {
		localRouter := http.NewServeMux()
		metrics.Setup(localRouter)
		jobs.Setup(localRouter, cfg)
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

	cfg := config.NewEnvConfig(env.Get)

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
