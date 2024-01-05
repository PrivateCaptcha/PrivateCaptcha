//go:build !unittests

package api

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgxpool"
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

	// common.SetupLogs("test", false)

	var err error

	pool, err = db.Connect(context.TODO(), os.Getenv("PC_POSTGRES"))

	if err != nil {
		fmt.Printf("Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}

	defer pool.Close()

	queries = dbgen.New(pool)

	store := &db.Store{
		Queries: queries,
	}

	server = &Server{
		Auth: &AuthMiddleware{
			Store: store,
		},
		Prefix: "",
		Salt:   []byte("salt"),
	}

	fmt.Println("Migrations completed")

	// TODO: seed data

	os.Exit(m.Run())
}
