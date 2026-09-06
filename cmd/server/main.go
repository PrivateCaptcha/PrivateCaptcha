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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
	"github.com/PrivateCaptcha/PrivateCaptcha/widget"
	"github.com/justinas/alice"
	"github.com/medama-io/go-useragent"
)

const (
	modeMigrate             = "migrate"
	modeRollback            = "rollback"
	modeServer              = "server"
	modeAuto                = "auto"
	_readinessDrainDelay    = 5 * time.Second
	_shutdownHardPeriod     = 5 * time.Second
	_shutdownPeriod         = 10 * time.Second
	_dbConnectTimeout       = 30 * time.Second
	_sessionPersistInterval = 10 * time.Second
	_auditLogInterval       = 10 * time.Second
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
	// catch call defaults are even lower
	catchAllLeakyBucketCap = 2
	catchAllLeakInterval   = 30 * time.Second
	// local mode: stricter defaults
	localLeakyBucketCap = 8
	localLeakInterval   = 1 * time.Second
	maxCatchAllBuckets  = 1_000
)

var (
	GitCommit       string
	flagMode        = flag.String("mode", "", strings.Join([]string{modeMigrate, modeServer, modeRollback, modeAuto}, " | "))
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

	metrics := monitoring.NewService()

	pool, clickhouse, dberr := db.Connect(ctx, cfg, _dbConnectTimeout, false /*admin*/, metrics)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	businessDB := db.NewBusiness(pool)
	timeSeriesDB := db.NewTimeSeries(clickhouse, businessDB.Cache)

	puzzleVerifier := api.NewVerifier(cfg, businessDB, cfg.Get(common.FingerprintHeaderKey))

	metrics.RegisterPgxPoolStats(func() monitoring.PgxPoolStatProvider {
		return pool.Stat()
	})

	cdnURLConfig := config.AsURL(ctx, cfg.Get(common.CDNBaseURLKey))
	portalURLConfig := config.AsURL(ctx, cfg.Get(common.PortalBaseURLKey))

	sender := email.NewMailSender(cfg)
	uaParser := useragent.NewParser()
	mailer := portal.NewPortalMailer("https:"+cdnURLConfig.URL(), "https:"+portalURLConfig.URL(), sender, cfg, uaParser)

	rateLimitHeader := cfg.Get(common.RateLimitHeaderKey).Value()
	ipRateLimiter := ratelimit.NewIPAddrRateLimiter(rateLimitHeader, newIPAddrBuckets(cfg))
	userLimiter := api.NewUserLimiter(businessDB, planService)
	subscriptionLimits := db.NewSubscriptionLimits(stage, businessDB, planService)
	idHasher := common.NewIDHasher(cfg.Get(common.IDHasherSaltKey))

	// special case for async jobs (register handlers before adding)
	asyncTasksJob := maintenance.NewAsyncTasksJob(businessDB)

	rulesCompiler := rules.NewRulesCompiler(uaParser)
	noticeProvider := db.NewNoticeProvider(cfg.Get(common.WidgetNoticeKey))

	apiURLConfig := config.AsURL(ctx, cfg.Get(common.APIBaseURLKey))
	apiServer := &api.Server{
		Stage:               stage,
		Prefix:              apiURLConfig.Path(),
		BusinessDB:          businessDB,
		TimeSeries:          timeSeriesDB,
		RateLimiter:         ipRateLimiter,
		Auth:                api.NewAuthMiddleware(businessDB, userLimiter, planService, metrics, rulesCompiler),
		VerifyLogChan:       make(chan *common.VerifyRecord, 10*api.VerifyBatchSize),
		FormSubmitLogChan:   make(chan *common.FormSubmitRecord, 10*api.FormSubmitLogBatchSize),
		FormSubmissionChan:  make(chan *api.FormSubmission, 10*api.FormBatchSize),
		Verifier:            puzzleVerifier,
		FormURLVerifier:     api.NewFormURLVerifier(),
		Metrics:             metrics,
		Mailer:              mailer,
		Levels:              difficulty.NewLevels(timeSeriesDB, 100 /*levelsBatchSize*/, api.PropertyBucketSize),
		VerifyLogCancel:     func() {},
		FormSubmitLogCancel: func() {},
		SubscriptionLimits:  subscriptionLimits,
		IDHasher:            idHasher,
		AsyncTasks:          asyncTasksJob,
		CountryCodeHeader:   cfg.Get(common.CountryCodeHeaderKey),
		NoticeProvider:      noticeProvider,
	}
	apiServerCfg := api.ServerConfig{
		VerifyFlushInterval: 10 * time.Second,
		AuthBackfillDelay:   1 * time.Second,
		FormFlushInterval:   50 * time.Millisecond,
		BackpressureTimeout: 5 * time.Second,
	}
	if err := apiServer.Init(ctx, apiServerCfg); err != nil {
		return err
	}

	dataCtx, err := web.LoadData()
	if err != nil {
		return err
	}

	// so far tips are disabled
	//tips, err := web.LoadTips()
	tips, err := []*web.Tip{}, nil
	if err != nil {
		slog.ErrorContext(ctx, "Failed to load tips", common.ErrAttr(err))
	}

	healthCheck := &maintenance.HealthCheckJob{
		BusinessDB:      businessDB,
		TimeSeriesDB:    timeSeriesDB,
		CheckInterval:   cfg.Get(common.HealthCheckIntervalKey),
		MaintenanceMode: cfg.Get(common.MaintenanceModeKey),
		Metrics:         metrics,
	}
	var closeQuitOnce sync.Once
	quit := make(chan struct{})
	quitFunc := func(ctx context.Context, immediately bool) {
		slog.DebugContext(ctx, "Server quit triggered", "immediately", immediately)
		healthCheck.Shutdown(ctx)
		healthCheckTime := 1 * time.Second

		if !immediately {
			healthCheckTime = min(_readinessDrainDelay, healthCheck.Interval())
			persistCtx, cancel := context.WithTimeout(context.Background(), healthCheckTime)
			defer cancel()
			go common.RunAdHocFunc(persistCtx, func(bctx context.Context) error {
				cacheDir := cfg.Get(common.CacheDirKey).Value()
				var errs []error
				if err := businessDB.SaveCache(bctx, cacheDir); err != nil {
					errs = append(errs, err)
				}
				if err := ipRateLimiter.SaveCache(bctx, cacheDir); err != nil {
					errs = append(errs, err)
				}
				if err := apiServer.Levels.SaveCache(bctx, cacheDir); err != nil {
					errs = append(errs, err)
				}
				if len(errs) > 0 {
					return fmt.Errorf("cache save errors: %v", errs)
				}
				return nil
			})
		}

		// Give time for readiness check to propagate
		time.Sleep(healthCheckTime)
		closeQuitOnce.Do(func() { close(quit) })
	}
	checkLicenseJob, err := maintenance.NewCheckLicenseJob(businessDB, cfg, GitCommit, quitFunc)
	if err != nil {
		return err
	}

	emailVerifier := &portal.PortalEmailVerifier{}
	sessionStore := db.NewSessionStore(businessDB, session.KeyPersistent, metrics)
	xsrfKey := cfg.Get(common.XSRFKeyKey)
	portalServer := &portal.Server{
		Stage:      stage,
		Prefix:     portalURLConfig.Path(),
		Store:      businessDB,
		TimeSeries: timeSeriesDB,
		XSRF:       &common.XSRFMiddleware{Key: xsrfKey.Value(), Timeout: 1 * time.Hour},
		Sessions: &session.Manager{
			CookieName:   "pcsid",
			Store:        sessionStore,
			MaxLifetime:  sessionStore.TTL(),
			SecureCookie: (*certFileFlag != "") && (*keyFileFlag != ""),
		},
		PlanService:        planService,
		APIURL:             apiURLConfig.URL(),
		CDNURL:             cdnURLConfig.URL(),
		PuzzleEngine:       apiServer.ReportingVerifier(),
		Metrics:            metrics,
		Mailer:             mailer,
		RateLimiter:        ipRateLimiter,
		DataCtx:            dataCtx,
		Tips:               tips,
		IDHasher:           idHasher,
		AdminEmail:         cfg.Get(common.AdminEmailKey),
		CountryCodeHeader:  cfg.Get(common.CountryCodeHeaderKey),
		UserLimiter:        userLimiter,
		SubscriptionLimits: subscriptionLimits,
		EmailVerifier:      emailVerifier,
		FormURLVerifier:    apiServer.FormURLVerifier,
		TwoFactorDuration:  10*time.Minute + 5*time.Minute,
		LicenseService:     checkLicenseJob,
	}

	templatesBuilder := portal.NewTemplatesBuilder()
	if err := templatesBuilder.AddFS(ctx, web.Templates(), "core"); err != nil {
		return err
	}

	if err := portalServer.Init(ctx, templatesBuilder, GitCommit, _sessionPersistInterval); err != nil {
		return err
	}

	updateConfigFunc := func(ctx context.Context) {
		cfg.Update(ctx)
		updateIPBuckets(cfg, ipRateLimiter)
		maintenanceMode := config.AsBool(cfg.Get(common.MaintenanceModeKey))
		businessDB.UpdateConfig(maintenanceMode)
		timeSeriesDB.UpdateConfig(maintenanceMode)
		portalServer.UpdateConfig(ctx, cfg)
		verboseLogs := config.AsBool(cfg.Get(common.VerboseKey))
		common.SetLogLevel(logLevel, verboseLogs)
		noticeProvider.Update()
	}
	updateConfigFunc(ctx)

	// nolint:errcheck
	go common.RunPeriodicJobOnce(common.TraceContext(context.Background(), "check_license"), checkLicenseJob, checkLicenseJob.NewParams(), 1*time.Second)

	router := http.NewServeMux()
	apiServer.Setup(apiURLConfig.Domain(), verbose, common.NoopMiddleware).Register(router)
	portalDomain := portalURLConfig.Domain()
	portalServer.Setup(portalDomain, common.NoopMiddleware).Register(router)
	rateLimiter := ipRateLimiter.RateLimitExFunc(publicLeakyBucketCap, publicLeakInterval)
	cdnPrefix := cdnURLConfig.Domain()
	cdnPath := cdnURLConfig.Path()
	if len(cdnPath) > 0 {
		cdnPrefix = cdnURLConfig.Domain() + cdnPath
	}
	recovered := common.Recovered(metrics)
	cdnChain := alice.New(recovered, metrics.CDNHandler, rateLimiter)
	router.Handle("GET "+cdnPrefix+"/portal/", http.StripPrefix(cdnPath+"/portal/", cdnChain.Then(web.Static(GitCommit))))
	router.Handle("GET "+cdnPrefix+"/widget/", http.StripPrefix(cdnPath+"/widget/", cdnChain.Then(widget.Static(GitCommit))))
	// "protection" (NOTE: different than usual order of monitoring)
	publicChain := alice.New(recovered, metrics.IgnoredHandler, rateLimiter)
	portalServer.SetupCatchAll(router, portalDomain, publicChain)
	// catch all routes with stricter limit
	catchAllRateLimiter := ratelimit.NewIPAddrRateLimiter(rateLimitHeader,
		ratelimit.NewIPAddrBuckets(maxCatchAllBuckets, catchAllLeakyBucketCap, catchAllLeakInterval))
	catchAllChain := alice.New(recovered, metrics.IgnoredHandler, catchAllRateLimiter.RateLimit)
	router.Handle("/", catchAllChain.ThenFunc(common.CatchAll))

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
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
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
				quitFunc(ctx, false /*immediately*/)
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

	go common.RunAdHocFunc(ctx, func(ctx context.Context) error {
		cacheDir := cfg.Get(common.CacheDirKey).Value()
		var errs []error
		if err := businessDB.LoadCache(ctx, cacheDir); err != nil {
			errs = append(errs, err)
		}
		if err := ipRateLimiter.LoadCache(ctx, cacheDir); err != nil {
			errs = append(errs, err)
		}
		if err := apiServer.Levels.LoadCache(ctx, cacheDir); err != nil {
			errs = append(errs, err)
		}
		if len(errs) > 0 {
			return fmt.Errorf("cache load errors: %v", errs)
		}
		return nil
	})

	businessDB.Start(ctx, _auditLogInterval)

	jobConcurrency := config.AsInt(cfg.Get(common.MaintenanceJobConcurrencyKey), 2)
	jobs := maintenance.NewJobs(businessDB, jobConcurrency)

	// nolint:staticcheck
	fetchBrowserVersions, refreshBrowserVersions := maintenance.NewBrowserVersionJobs(businessDB, rulesCompiler.BrowserVersions())
	if refreshBrowserVersions != nil { // nolint:staticcheck
		// nolint:errcheck
		go common.RunPeriodicJobOnce(context.Background(), refreshBrowserVersions, refreshBrowserVersions.NewParams(), 1*time.Second)
	}

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
	jobs.AddLocked(30*time.Minute, &maintenance.UserEmailNotificationsJob{
		RunInterval:   3 * time.Hour, // overlap few locked intervals to cover for possible unprocessed notifications
		Store:         businessDB,
		Templates:     email.Templates(),
		Sender:        sender,
		ChunkSize:     50,
		MaxAttempts:   5,
		ReplyToEmail:  cfg.Get(common.ReplyToEmailKey),
		PlanService:   planService,
		EmailVerifier: emailVerifier,
		CDNURL:        mailer.CDNURL,
		PortalURL:     mailer.PortalURL,
	})
	jobs.AddLocked(24*time.Hour, &maintenance.CleanupUserNotificationsJob{
		Store:              businessDB,
		NotificationMonths: 6,
		TemplateMonths:     7,
	})
	jobs.AddLocked(3*time.Hour, &maintenance.ExpireInternalTrialsJob{
		PastInterval: 3 * time.Hour,
		Age:          24 * time.Hour,
		BusinessDB:   businessDB,
		PlanService:  planService,
	})
	jobs.AddLocked(24*time.Hour, &maintenance.CleanupAuditLogJob{
		PastInterval: portal.MaxAuditLogsRetention(cfg),
		BusinessDB:   businessDB,
	})
	jobs.AddLocked(24*time.Hour, &maintenance.CleanupAsyncTasksJob{
		PastInterval: 30 * 24 * time.Hour,
		BusinessDB:   businessDB,
	})
	jobs.AddLocked(6*time.Hour, &maintenance.ScheduleReportsJob{
		Store:       businessDB,
		TimeSeries:  timeSeriesDB,
		PlanService: planService,
		PortalURL:   mailer.PortalURL,
		IDHasher:    idHasher,
		Stage:       stage,
		UsersLimit:  50,
	})
	jobs.AddLocked(3*time.Hour, &maintenance.DeactivateFailingFormsJob{
		Store:      businessDB,
		TimeSeries: timeSeriesDB,
		PortalURL:  mailer.PortalURL,
		IDHasher:   idHasher,
	})
	jobs.AddLocked(10*time.Minute, asyncTasksJob)
	jobs.AddLocked(24*time.Hour, fetchBrowserVersions)
	jobs.Add(refreshBrowserVersions)

	jobs.RunAll()

	var localServer *http.Server
	if localAddress := cfg.Get(common.LocalAddressKey).Value(); len(localAddress) > 0 {
		localRouter := http.NewServeMux()
		metrics.Setup(localRouter)
		localAPIKey := cfg.Get(common.LocalAPIKeyKey).Value()
		localRateLimiter := ipRateLimiter.RateLimitExFunc(localLeakyBucketCap, localLeakInterval)
		localMiddleware := alice.New(common.ServiceMiddleware("local"), recovered, localRateLimiter, common.APIKeyMiddleware(localAPIKey), monitoring.Logged)
		jobs.Setup(localRouter, localMiddleware)
		localRouter.Handle(http.MethodDelete+" /"+common.CacheEndpoint, localMiddleware.ThenFunc(func(http.ResponseWriter, *http.Request) { businessDB.ClearCache() }))
		localRouter.Handle(http.MethodGet+" /"+common.LiveEndpoint, recovered(http.HandlerFunc(healthCheck.LiveHandler)))
		localRouter.Handle(http.MethodGet+" /"+common.ReadyEndpoint, recovered(http.HandlerFunc(healthCheck.ReadyHandler)))
		localServer = &http.Server{
			Addr:              localAddress,
			Handler:           localRouter,
			ReadHeaderTimeout: 5 * time.Second,
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
		jobs.Stop()
		sessionStore.Stop()
		apiServer.Stop()
		businessDB.Stop()
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
		sessionStore.Shutdown()
		apiServer.Shutdown()
		businessDB.Shutdown()
		if localServer != nil {
			if lerr := localServer.Close(); lerr != nil {
				slog.ErrorContext(ctx, "Failed to shutdown local server", common.ErrAttr(lerr))
			}
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

	pool, clickhouse, dberr := db.Connect(ctx, cfg, _dbConnectTimeout, true /*admin*/, nil)
	if pool != nil {
		defer pool.Close()
	}
	if clickhouse != nil {
		defer clickhouse.Close()
	}
	if dberr != nil {
		return dberr
	}

	if pool != nil {
		if err := db.MigratePostgres(ctx, pool, cfg, planService, up); err != nil {
			return err
		}
	}

	if clickhouse != nil {
		if err := db.MigrateClickHouse(ctx, clickhouse, cfg, up); err != nil {
			return err
		}
	}

	return nil
}

func serve(cfg common.ConfigStore) (err error) {
	ctx := common.TraceContext(context.Background(), "main")
	if listener, lerr := createListener(ctx, cfg); lerr == nil {
		err = run(ctx, cfg, os.Stderr, listener)
	} else {
		err = lerr
	}
	return
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
		os.Exit(1)
	}

	cfg := config.NewEnvConfig(env.Get)

	switch *flagMode {
	case modeServer:
		err = serve(cfg)
	case modeMigrate:
		mctx := common.TraceContext(context.Background(), "migration")
		err = migrate(mctx, cfg, true /*up*/)
	case modeRollback:
		rctx := common.TraceContext(context.Background(), "migration")
		err = migrate(rctx, cfg, false /*up*/)
	case modeAuto:
		mctx := common.TraceContext(context.Background(), "migration")
		if err = migrate(mctx, cfg, true /*up*/); err == nil {
			err = serve(cfg)
		}
	default:
		err = fmt.Errorf("unknown mode: '%s'", *flagMode)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
