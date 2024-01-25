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
	cache   common.Cache
)

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	common.SetupLogs("test", true)

	var err error

	pool, err = db.ConnectPostgres(context.TODO(), os.Getenv("PC_POSTGRES"))
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	opts, err := redis.ParseURL(os.Getenv("PC_REDIS"))
	if err != nil {
		panic(err)
	}
	dbCache := db.NewRedisCache(opts)
	err = dbCache.Ping(context.TODO())
	if err != nil {
		panic(err)
	}
	cache = dbCache

	queries = dbgen.New(pool)

	store := db.NewStore(queries, cache)

	server = &Server{
		Auth: &AuthMiddleware{
			Store: store,
		},
		Store:  store,
		Prefix: "",
		Salt:   []byte("salt"),
	}

	fmt.Println("Migrations completed")

	// TODO: seed data

	os.Exit(m.Run())
}
