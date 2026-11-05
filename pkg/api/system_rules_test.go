//go:build !enterprise

package api

import (
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/medama-io/go-useragent"
)

// TestInitSystemRulesNotStale guards against the regression where system
// rules are compiled with a nil UA parser, which makes the "User-Agent bot
// detection" rule permanently stale and silently skipped. The UA parser
// from Server.Verifier must be wired into the system-rules compiler.
func TestInitSystemRulesNotStale(t *testing.T) {
	cfg := testsConfigStore()
	verifier := NewVerifier(cfg, nil, cfg.Get(common.FingerprintHeaderKey), useragent.NewParser())
	srv := &Server{Verifier: verifier}
	srv.initSystemRules(t.Context())
	if srv.SystemRules == nil {
		t.Fatal("system rules are nil after initSystemRules")
	}
	if srv.SystemRules.Empty() {
		t.Fatal("system rules are empty after initSystemRules")
	}
	if srv.SystemRules.IsStale() {
		t.Fatal("system rules are stale; the UA bot-detection rule must compile with a non-nil parser")
	}
}
