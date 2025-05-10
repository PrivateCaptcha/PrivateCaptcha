//go:build !unittests

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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	s          *Server
	cfg        common.ConfigStore
	cache      common.Cache[db.CacheKey, any]
	timeSeries *db.TimeSeriesStore
	store      *db.BusinessStore
)

const (
	authBackfillDelay   = 100 * time.Millisecond
	verifyFlushInterval = 1 * time.Second
)

func testsConfigStore() common.ConfigStore {
	baseCfg := config.NewBaseConfig(config.NewEnvConfig(config.DefaultMapper, os.Getenv))
	baseCfg.Add(config.NewStaticValue(common.PuzzleLeakyBucketBurstKey, "20"))
	baseCfg.Add(config.NewStaticValue(common.DefaultLeakyBucketBurstKey, "20"))
	baseCfg.Add(config.NewStaticValue(common.PuzzleLeakyBucketRateKey, "10"))
	baseCfg.Add(config.NewStaticValue(common.DefaultLeakyBucketRateKey, "10"))
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
	pool, clickhouse, dberr = db.Connect(context.Background(), cfg)
	if dberr != nil {
		panic(dberr)
	}

	timeSeries = db.NewTimeSeries(clickhouse)

	var err error
	cache, err = db.NewMemoryCache[db.CacheKey, any](100, nil)
	if err != nil {
		panic(err)
	}

	store = db.NewBusinessEx(pool, cache)

	planService := billing.NewPlanService()

	s = &Server{
		Stage:              common.StageTest,
		BusinessDB:         store,
		TimeSeries:         timeSeries,
		Auth:               NewAuthMiddleware(cfg, store, authBackfillDelay, planService),
		VerifyLogChan:      make(chan *common.VerifyRecord, 10*VerifyBatchSize),
		Salt:               NewPuzzleSalt(cfg.Get(common.APISaltKey)),
		UserFingerprintKey: NewUserFingerprintKey(cfg.Get(common.UserFingerprintIVKey)),
		PaddleAPI:          &billing.StubPaddleClient{},
		PlanService:        planService,
		Metrics:            monitoring.NewStub(),
		Mailer:             &email.StubMailer{},
		Levels:             difficulty.NewLevels(timeSeries, 100 /*levelsBatchSize*/, PropertyBucketSize),
		VerifyLogCancel:    func() {},
	}
	if err := s.Init(context.TODO(), verifyFlushInterval); err != nil {
		panic(err)
	}
	defer s.Shutdown()

	// TODO: seed data

	os.Exit(m.Run())
}
