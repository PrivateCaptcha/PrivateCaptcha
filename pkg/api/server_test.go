//go:build !unittests

package api

import (
	"context"
	"database/sql"
	"flag"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	s          *server
	cache      common.Cache[string, any]
	timeSeries *db.TimeSeriesStore
	auth       *authMiddleware
	store      *db.BusinessStore
)

const (
	authBackfillDelay = 100 * time.Millisecond
)

func fakeRateLimiter(next http.HandlerFunc) http.HandlerFunc {
	return next
}

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs("test", true)

	var err error
	var pool *pgxpool.Pool
	var clickhouse *sql.DB
	var dberr error
	pool, clickhouse, dberr = db.Connect(context.Background(), os.Getenv)
	if dberr != nil {
		panic(dberr)
	}

	timeSeries = db.NewTimeSeries(clickhouse)

	cache, err = db.NewMemoryCache[string, any](1*time.Minute, 100, nil)
	if err != nil {
		panic(err)
	}

	store = db.NewBusiness(pool, cache)

	ratelimiter := ratelimit.NewIPAddrRateLimiter(os.Getenv(common.ConfigRateLimitHeader))

	blockedUsers := db.NewStaticCache[int32, *common.UserLimitStatus](100 /*cap*/, nil /*missing data*/)
	auth = NewAuthMiddleware(os.Getenv, store, ratelimiter, blockedUsers, authBackfillDelay)
	defer auth.Shutdown()

	s = NewServer(store, timeSeries, auth, 2*time.Second /*flush interval*/, &billing.StubPaddleClient{}, monitoring.NewStub(), os.Getenv)
	defer s.Shutdown()

	// TODO: seed data

	os.Exit(m.Run())
}
