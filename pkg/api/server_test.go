//go:build !unittests

package api

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var (
	pool    *pgxpool.Pool
	server  *Server
	queries *dbgen.Queries
)

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs("test", true)

	var err error

	pool, err = db.Connect(context.TODO(), os.Getenv("PC_POSTGRES"))

	if err != nil {
		panic(err)
	}

	opts, err := redis.ParseURL(os.Getenv("PC_REDIS"))
	if err != nil {
		panic(err)
	}
	rdb := redis.NewClient(opts)

	cache := &db.Cache{
		Redis: rdb,
	}

	defer pool.Close()

	queries = dbgen.New(pool)

	store := &db.Store{
		Queries: queries,
	}

	server = &Server{
		Auth: &AuthMiddleware{
			Store: store,
			Cache: cache,
		},
		Prefix: "",
		Salt:   []byte("salt"),
	}

	fmt.Println("Migrations completed")

	// TODO: seed data

	os.Exit(m.Run())
}
