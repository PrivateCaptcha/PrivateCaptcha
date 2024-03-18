package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"log/slog"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session/providers/memory"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

var (
	GitCommit string
)

const (
	propertyBucketSize = 5 * time.Minute
	levelsBatchSize    = 100
)

func run(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	stage := getenv("STAGE")
	common.SetupLogs(stage, getenv("VERBOSE") == "1")

	pool, clickhouse, dberr := db.Migrate(getenv)
	if dberr != nil {
		return dberr
	}

	defer pool.Close()
	defer clickhouse.Close()

	store := db.NewStore(dbgen.New(pool), db.NewMemoryCache(1*time.Minute), 5*time.Minute)

	levels := difficulty.NewLevels(clickhouse, levelsBatchSize, propertyBucketSize)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	apiServer := &api.Server{
		Auth: &api.AuthMiddleware{
			Store: store,
		},
		Store:  store,
		Levels: levels,
		Prefix: "api",
		Salt:   []byte("salt"),
	}

	if byteArray, err := hex.DecodeString(getenv("UA_KEY")); (err == nil) && (len(byteArray) == 64) {
		copy(apiServer.UAKey[:], byteArray[:])
	} else {
		slog.Error("Error initializing UA key for server", common.ErrAttr(err), "size", len(byteArray))
	}

	portalServer := &portal.Server{
		Store:  store,
		Prefix: "portal",
		XSRF:   portal.XSRFMiddleware{Key: "key", Timeout: 24 * time.Hour},
		Session: session.Manager{
			CookieName:  "sid",
			Provider:    memory.New(),
			MaxLifetime: 24 * time.Hour,
		},
	}

	router := http.NewServeMux()

	apiServer.Setup(router)
	portalServer.Setup(router)
	router.Handle("/", web.Static())

	host := getenv("PC_HOST")
	if host == "" {
		host = "localhost"
	}

	port := getenv("PC_PORT")
	if port == "" {
		port = "8080"
	}

	httpServer := &http.Server{
		Addr:              net.JoinHostPort(host, port),
		Handler:           router,
		ReadHeaderTimeout: 4 * time.Second,
		ReadTimeout:       10 * time.Second,
		MaxHeaderBytes:    256 * 1024,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		slog.Info("Listening", "address", httpServer.Addr, "version", GitCommit)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Error listening and serving", common.ErrAttr(err))
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		slog.Debug("Shutting down gracefully...")
		levels.Shutdown()
		store.Shutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "error shutting down http server: %s\n", err)
		}
		slog.Debug("Shutdown finished")
	}()

	wg.Wait()
	return nil
}

func main() {
	if err := run(context.Background(), os.Getenv, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
