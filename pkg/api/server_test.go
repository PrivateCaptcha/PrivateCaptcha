package api

import (
	"runtime/debug"

	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"context"
	"database/sql"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/medama-io/go-useragent"
)

var (
	server     *Server
	cfg        common.ConfigStore
	cache      common.Cache[db.CacheKey, any]
	timeSeries common.TimeSeriesStore
	store      *db.BusinessStore
	testPlan   billing.Plan
)

const (
	authBackfillDelay   = 100 * time.Millisecond
	verifyFlushInterval = 1 * time.Second
)

func testsConfigStore() common.ConfigStore {
	baseCfg := config.NewBaseConfig(config.NewEnvConfig(os.Getenv))
	baseCfg.Add(config.NewStaticValue(common.RateLimitBurstKey, "20"))
	baseCfg.Add(config.NewStaticValue(common.RateLimitRateKey, "10"))
	baseCfg.Add(config.NewStaticValue(common.ClickHouseOptionalKey, "true"))
	baseCfg.Add(config.NewStaticValue(common.CountryCodeHeaderKey, "CF-IPCountry"))
	return baseCfg
}

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs(common.StageTest, true)

	cfg = testsConfigStore()

	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Connect(context.Background(), cfg, 3*time.Second, false /*admin*/, nil)
	if dberr != nil {
		panic(dberr)
	}

	if clickhouse != nil {
		timeSeries = db.NewTimeSeries(clickhouse, cache)
	} else {
		timeSeries = db.NewMemoryTimeSeries()
	}

	var err error
	cache, err = db.NewMemoryCache[db.CacheKey, any]("default", 100_000, &struct{}{}, 1*time.Minute, 3*time.Minute, 30*time.Second)
	if err != nil {
		panic(err)
	}

	store = db.NewBusinessEx(pool, cache)

	metrics := monitoring.NewStub()

	planService := billing.NewPlanService(nil)
	testPlan = planService.GetInternalTrialPlan()

	server = &Server{
		Stage:              common.StageTest,
		BusinessDB:         store,
		TimeSeries:         timeSeries,
		RateLimiter:        &ratelimit.StubRateLimiter{Header: cfg.Get(common.RateLimitHeaderKey).Value()},
		Auth:               NewAuthMiddleware(store, NewUserLimiter(store), planService, metrics, rules.NewRulesCompiler(useragent.NewParser())),
		VerifyLogChan:      make(chan *common.VerifyRecord, 10*VerifyBatchSize),
		Verifier:           NewVerifier(cfg, store, cfg.Get(common.FingerprintHeaderKey)),
		Metrics:            metrics,
		Mailer:             &email.StubMailer{},
		Levels:             difficulty.NewLevels(timeSeries, 100 /*levelsBatchSize*/, PropertyBucketSize),
		VerifyLogCancel:    func() {},
		SubscriptionLimits: db.NewSubscriptionLimits(common.StageTest, store, planService),
		IDHasher:           common.NewIDHasher(cfg.Get(common.IDHasherSaltKey)),
		AsyncTasks:         maintenance.NewAsyncTasksJob(store),
		CountryCodeHeader:  cfg.Get(common.CountryCodeHeaderKey),
		NoticeProvider:     &db_tests.StubNoticeProvider{},
	}
	if err := server.Init(context.TODO(), verifyFlushInterval, authBackfillDelay, 100*time.Millisecond); err != nil {
		panic(err)
	}
	defer server.Shutdown()

	// TODO: seed data

	exitCode := m.Run()
	if exitCode == 0 {
		exitCode = db_tests.TestCacheSerialization(store)
	}
	os.Exit(exitCode)
}

func TestAPIServerStoreErrors(t *testing.T) {
	expectedErr := errors.New("generic db error")
	stub := &db.QuerierStub{Error: expectedErr}
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})

	// Create BusinessStore wrapping our stub querier
	store := db.NewBusinessWithQuerier(nil, stub, cache)

	metrics := monitoring.NewStub()
	planService := billing.NewPlanService(nil)

	srv := &Server{
		Stage:              common.StageTest,
		BusinessDB:         store,
		TimeSeries:         db.NewMemoryTimeSeries(),
		RateLimiter:        &ratelimit.StubRateLimiter{Header: "X-Forwarded-For"},
		Auth:               NewAuthMiddleware(store, NewUserLimiter(store), planService, metrics, rules.NewRulesCompiler(useragent.NewParser())),
		VerifyLogChan:      make(chan *common.VerifyRecord, 10),
		Verifier:           NewVerifier(testsConfigStore(), store, config.NewStaticValue(common.FingerprintHeaderKey, "FP")),
		Metrics:            metrics,
		Mailer:             &email.StubMailer{},
		Levels:             difficulty.NewLevels(db.NewMemoryTimeSeries(), 100, PropertyBucketSize),
		VerifyLogCancel:    func() {},
		SubscriptionLimits: db.NewSubscriptionLimits(common.StageTest, store, planService),
		IDHasher:           common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "salt")),
		AsyncTasks:         maintenance.NewAsyncTasksJob(store),
		CountryCodeHeader:  config.NewStaticValue(common.CountryCodeHeaderKey, "CF"),
		NoticeProvider:     &db_tests.StubNoticeProvider{},
	}

	srv.APIHeaders = make(map[string][]string)
	rg := srv.Setup("/api", false, common.NoopMiddleware)
	mux := http.NewServeMux()
	rg.Register(mux)

	for _, route := range rg.Routes() {
		parts := strings.SplitN(route.Prefix, " ", 2)
		var method, path string
		if len(parts) == 2 {
			method = parts[0]
			path = parts[1] + route.Path
		} else {
			method = "GET"
			path = parts[0] + route.Path
		}
		path = strings.ReplaceAll(path, "{$}", "")
		path = strings.ReplaceAll(path, "{id}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{orgID}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{propertyID}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{org}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{property}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{rule}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{user}", srv.IDHasher.Encrypt(1))
		path = strings.ReplaceAll(path, "{key}", "123")
		path = strings.ReplaceAll(path, "{period}", common.TimePeriodMonth.String())
		path = strings.ReplaceAll(path, "{tab}", common.GeneralEndpoint)
		path = strings.ReplaceAll(path, "{difficulty}", string(dbgen.DifficultyGrowthMedium))
		path = strings.ReplaceAll(path, "{code}", "429")

		req := httptest.NewRequest(method, path, nil)
		secret := db.UUIDToSecret(*randomUUID())
		req.Header.Set(common.HeaderAPIKey, secret)
		req.PostForm = map[string][]string{common.ParamSecret: {secret}}
		req.RemoteAddr = "127.0.0.1:1234"

		w := httptest.NewRecorder()

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Route %s %s panicked: %v\n%s", method, path, r, debug.Stack())
			}
		}()

		mux.ServeHTTP(w, req)

		if w.Code < 400 && w.Code != http.StatusNotFound && method != "OPTIONS" {
			t.Errorf("Route %s %s expected error status, got %d", method, path, w.Code)
		}
	}
}
