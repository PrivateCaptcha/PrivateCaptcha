package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/medama-io/go-useragent"
)

const (
	normalUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// newSystemRulesServer creates a Server with an Auth middleware whose
// RulesCompiler is backed by a real user-agent parser, matching the
// production enterprise configuration. initSystemRules is then called
// so that s.SystemRules reflects the result of the startup code path.
func newSystemRulesServer(t *testing.T) *Server {
	t.Helper()
	parser := useragent.NewParser()
	srv := &Server{
		Auth: NewAuthMiddleware(nil, nil, billing.NewPlanService(nil), monitoring.NewStub(), rules.NewRulesCompiler(parser)),
	}
	srv.initSystemRules(t.Context())
	return srv
}

func TestInitSystemRulesPopulates(t *testing.T) {
	srv := newSystemRulesServer(t)

	if srv.SystemRules == nil {
		t.Fatal("SystemRules is nil after initSystemRules")
	}
	if srv.SystemRules.Empty() {
		t.Fatal("SystemRules is empty after initSystemRules")
	}
}

func TestInitSystemRulesAutomationDetection(t *testing.T) {
	srv := newSystemRulesServer(t)

	property := difficulty.NewStubProperty(1, true, 1, 1, int16(common.DifficultyLevelMedium), dbgen.DifficultyGrowthMedium)
	req := httptest.NewRequest(http.MethodGet, "/puzzle", nil)
	req.Header.Set("User-Agent", normalUserAgent)
	req.Header.Set(common.HeaderClientHints, "automation")
	ri := rules.NewRequestInfo(req, "")

	applied, _ := srv.SystemRules.Apply(ri, property)
	expectedLevel := int16(common.DifficultyLevelMedium + 3*common.DifficultyDelta/2)
	if applied.Level() != expectedLevel {
		t.Fatalf("applied level = %d, want %d", applied.Level(), expectedLevel)
	}
	if applied.RuleID() != 0 {
		t.Fatalf("applied rule ID = %d, want 0 (excluded from statistics)", applied.RuleID())
	}
}

func TestInitSystemRulesNoMatchWithoutHints(t *testing.T) {
	srv := newSystemRulesServer(t)

	property := difficulty.NewStubProperty(1, true, 1, 1, int16(common.DifficultyLevelMedium), dbgen.DifficultyGrowthMedium)
	req := httptest.NewRequest(http.MethodGet, "/puzzle", nil)
	req.Header.Set("User-Agent", normalUserAgent)
	ri := rules.NewRequestInfo(req, "")

	applied, _ := srv.SystemRules.Apply(ri, property)
	if applied.Level() != property.Level() {
		t.Fatalf("applied level = %d, want unchanged %d", applied.Level(), property.Level())
	}
}

func TestInitSystemRulesUnknownHintDoesNotMatch(t *testing.T) {
	srv := newSystemRulesServer(t)

	property := difficulty.NewStubProperty(1, true, 1, 1, int16(common.DifficultyLevelMedium), dbgen.DifficultyGrowthMedium)
	req := httptest.NewRequest(http.MethodGet, "/puzzle", nil)
	req.Header.Set("User-Agent", normalUserAgent)
	req.Header.Set(common.HeaderClientHints, "unknown")
	ri := rules.NewRequestInfo(req, "")

	applied, _ := srv.SystemRules.Apply(ri, property)
	if applied.Level() != property.Level() {
		t.Fatalf("applied level = %d, want unchanged %d", applied.Level(), property.Level())
	}
}
