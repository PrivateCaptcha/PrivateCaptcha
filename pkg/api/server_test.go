package api

import (
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

	os.Exit(m.Run())
}
