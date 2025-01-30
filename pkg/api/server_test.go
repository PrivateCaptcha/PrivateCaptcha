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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	s          *server
	cfg        *config.Config
	cache      common.Cache[db.CacheKey, any]
	timeSeries *db.TimeSeriesStore
	auth       *authMiddleware
	store      *db.BusinessStore
)

const (
	authBackfillDelay = 100 * time.Millisecond
)

func testsEnv(s string) string {
	switch s {
	case "PUZZLE_LEAKY_BUCKET_BURST", "DEFAULT_LEAKY_BUCKET_BURST":
		return "20"
	case "PUZZLE_LEAKY_BUCKET_RPS", "DEFAULT_LEAKY_BUCKET_RPS":
		return "10"
	default:
		return os.Getenv(s)
	}
}

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs(common.StageTest, true)

	var err error
	cfg, err = config.New(testsEnv)
	if err != nil {
		panic(err)
	}

	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Connect(context.Background(), cfg)
	if dberr != nil {
		panic(dberr)
	}

	timeSeries = db.NewTimeSeries(clickhouse)

	cache, err = db.NewMemoryCache[db.CacheKey, any](100, nil)
	if err != nil {
		panic(err)
	}

	store = db.NewBusiness(pool, cache)

	blockedUsers := db.NewStaticCache[int32, *common.UserLimitStatus](100 /*cap*/, nil /*missing data*/)
	auth = NewAuthMiddleware(cfg, store, blockedUsers, authBackfillDelay)
	defer auth.Shutdown()

	s = NewServer(store, timeSeries, auth, 2*time.Second /*flush interval*/, &billing.StubPaddleClient{}, monitoring.NewStub(), &email.StubMailer{}, os.Getenv)
	defer s.Shutdown()

	// TODO: seed data

	os.Exit(m.Run())
}
