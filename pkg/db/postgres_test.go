package db

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	config_pkg "github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
)

func TestTruncatedArgsShort(t *testing.T) {
	args := truncatedArgs{1, "hello", true}
	result := args.LogValue()
	if result.Kind() != slog.KindString {
		t.Fatalf("expected string kind, got %v", result.Kind())
	}
	s := result.String()
	if strings.HasSuffix(s, "...") {
		t.Fatalf("short args should not be truncated, got %q", s)
	}
	if !strings.Contains(s, "hello") {
		t.Fatalf("expected args to contain 'hello', got %q", s)
	}
}

func TestTruncatedArgsLong(t *testing.T) {
	longStr := strings.Repeat("a", 300)
	args := truncatedArgs{longStr}
	result := args.LogValue()
	if result.Kind() != slog.KindString {
		t.Fatalf("expected string kind, got %v", result.Kind())
	}
	s := result.String()
	if !strings.HasSuffix(s, "...") {
		t.Fatalf("long args should be truncated, got %q", s)
	}
	// maxArgsLogLength (200) + "..." (3) = 203
	if len(s) != maxArgsLogLength+3 {
		t.Fatalf("expected length %d, got %d", maxArgsLogLength+3, len(s))
	}
}

func TestTruncatedArgsEmpty(t *testing.T) {
	args := truncatedArgs{}
	result := args.LogValue()
	s := result.String()
	if s != "[]" {
		t.Fatalf("expected '[]', got %q", s)
	}
}

// stubConfigStore builds a ConfigStore that returns pgURL for the Postgres
// connection string and empty strings for every other key, mimicking the
// minimal configuration needed to exercise createPgxConfig without a live DB.
func stubConfigStore(t *testing.T, pgURL string) common.ConfigStore {
	t.Helper()
	fallback := config_pkg.NewEnvConfig(func(string) string { return "" })
	base := config_pkg.NewBaseConfig(fallback)
	base.Add(config_pkg.NewStaticValue(common.PostgresKey, pgURL))
	if len(pgURL) == 0 {
		base.Add(config_pkg.NewStaticValue(common.PostgresHostKey, "localhost"))
		base.Add(config_pkg.NewStaticValue(common.PostgresDBKey, "testdb"))
		base.Add(config_pkg.NewStaticValue(common.PostgresUserKey, "appuser"))
		base.Add(config_pkg.NewStaticValue(common.PostgresPasswordKey, "apppass"))
		base.Add(config_pkg.NewStaticValue(common.PostgresAdminKey, "adminuser"))
		base.Add(config_pkg.NewStaticValue(common.PostgresAdminPasswordKey, "adminpass"))
	}
	return base
}

func TestCreatePgxConfigRuntimeParams(t *testing.T) {
	const pgURL = "postgres://appuser:apppass@localhost:5432/testdb"
	wantIdle := strconv.Itoa(int(pgIdleInTransactionSessionTimeout.Milliseconds()))
	wantAppTimeout := strconv.Itoa(int(pgStatementTimeout.Milliseconds()))
	wantAppLock := strconv.Itoa(int(pgLockTimeout.Milliseconds()))

	tests := []struct {
		name          string
		migrate       bool
		wantStatement string
		wantLock      string
		wantIdleInTx  string
		wantAppName   string
	}{
		{
			name:          "MigrationPoolDisablesStatementAndLockTimeouts",
			migrate:       true,
			wantStatement: "0",
			wantLock:      "0",
			wantIdleInTx:  wantIdle,
			wantAppName:   "privatecaptcha",
		},
		{
			name:          "ApplicationPoolEnforcesStatementAndLockTimeouts",
			migrate:       false,
			wantStatement: wantAppTimeout,
			wantLock:      wantAppLock,
			wantIdleInTx:  wantIdle,
			wantAppName:   "privatecaptcha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := stubConfigStore(t, pgURL)
			config, err := createPgxConfig(ctx, cfg, tt.migrate, nil)
			if err != nil {
				t.Fatalf("createPgxConfig failed: %v", err)
			}
			if config == nil {
				t.Fatal("expected non-nil config")
			}
			rp := config.ConnConfig.RuntimeParams
			if rp == nil {
				t.Fatal("expected non-nil RuntimeParams")
			}
			if got := rp["statement_timeout"]; got != tt.wantStatement {
				t.Errorf("statement_timeout = %q, want %q", got, tt.wantStatement)
			}
			if got := rp["lock_timeout"]; got != tt.wantLock {
				t.Errorf("lock_timeout = %q, want %q", got, tt.wantLock)
			}
			if got := rp["idle_in_transaction_session_timeout"]; got != tt.wantIdleInTx {
				t.Errorf("idle_in_transaction_session_timeout = %q, want %q", got, tt.wantIdleInTx)
			}
			if got := rp["application_name"]; got != tt.wantAppName {
				t.Errorf("application_name = %q, want %q", got, tt.wantAppName)
			}
			if config.ConnConfig.Tracer == nil {
				t.Error("expected query tracer to be set on ConnConfig")
			}
		})
	}
}

func TestCreatePgxConfigEmptyURLUsesAdminCredsForMigrate(t *testing.T) {
	ctx := context.Background()
	cfg := stubConfigStore(t, "")
	config, err := createPgxConfig(ctx, cfg, true /*migrate*/, nil)
	if err != nil {
		t.Fatalf("createPgxConfig failed: %v", err)
	}
	if config == nil {
		t.Fatal("expected non-nil config")
	}
	if config.ConnConfig.User != "adminuser" {
		t.Errorf("expected admin user, got %q", config.ConnConfig.User)
	}
	if config.ConnConfig.Password != "adminpass" {
		t.Errorf("expected admin password, got %q", config.ConnConfig.Password)
	}
	rp := config.ConnConfig.RuntimeParams
	if got := rp["statement_timeout"]; got != "0" {
		t.Errorf("statement_timeout = %q, want %q (disabled on migration pool)", got, "0")
	}
	if got := rp["lock_timeout"]; got != "0" {
		t.Errorf("lock_timeout = %q, want %q (disabled on migration pool)", got, "0")
	}
}

func TestCreatePgxConfigEmptyURLUsesAppCredsForNonMigrate(t *testing.T) {
	ctx := context.Background()
	cfg := stubConfigStore(t, "")
	config, err := createPgxConfig(ctx, cfg, false /*migrate*/, nil)
	if err != nil {
		t.Fatalf("createPgxConfig failed: %v", err)
	}
	if config == nil {
		t.Fatal("expected non-nil config")
	}
	if config.ConnConfig.User != "appuser" {
		t.Errorf("expected app user, got %q", config.ConnConfig.User)
	}
	if config.ConnConfig.Password != "apppass" {
		t.Errorf("expected app password, got %q", config.ConnConfig.Password)
	}
	rp := config.ConnConfig.RuntimeParams
	want := strconv.Itoa(int(pgStatementTimeout.Milliseconds()))
	if got := rp["statement_timeout"]; got != want {
		t.Errorf("statement_timeout = %q, want %q (enforced on app pool)", got, want)
	}
}

func TestCreatePgxConfigInvalidURLReturnsError(t *testing.T) {
	ctx := context.Background()
	cfg := stubConfigStore(t, "postgres://user:pass@localhost:5432/db?application_name=%zz")
	_, err := createPgxConfig(ctx, cfg, false, nil)
	if err == nil {
		t.Fatal("expected error for invalid Postgres URL, got nil")
	}
}

// TestPgTimeoutConstantsAreTenSeconds guards against unintentional drift in
// the application-pool timeouts that the fix keys off. If these constants
// change, the RuntimeParams tests above must be updated in lockstep.
func TestPgTimeoutConstantsAreTenSeconds(t *testing.T) {
	if pgStatementTimeout != 10*time.Second {
		t.Errorf("pgStatementTimeout = %v, want 10s", pgStatementTimeout)
	}
	if pgLockTimeout != 10*time.Second {
		t.Errorf("pgLockTimeout = %v, want 10s", pgLockTimeout)
	}
	if pgIdleInTransactionSessionTimeout != 10*time.Second {
		t.Errorf("pgIdleInTransactionSessionTimeout = %v, want 10s", pgIdleInTransactionSessionTimeout)
	}
}
