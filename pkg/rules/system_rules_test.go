package rules

import (
	"context"
	"net/netip"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/medama-io/go-useragent"
)

// systemRulesDBRules mirrors the hard-coded system rules compiled by
// Server.initSystemRules in pkg/api/server_not_enterprise.go. Any change
// to those rules should be reflected here and in the companion test in
// pkg/api/system_rules_test.go.
func systemRulesDBRules() []*dbgen.DifficultyRule {
	return []*dbgen.DifficultyRule{
		{
			ID:                0,
			Name:              "Client-side automation detection",
			Enabled:           true,
			ConditionProperty: dbgen.RuleConditionPropertyClientSideDetection,
			ConditionOperator: dbgen.RuleConditionOperatorIn,
			ConditionValueStr: pgtype.Text{
				String: "automation",
				Valid:  true,
			},
			ActionProperty: dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:    300,
			Terminal:       false,
		},
		{
			ID:                0,
			Name:              "User-Agent bot detection",
			Enabled:           true,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorBot,
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       300,
			Terminal:          false,
		},
	}
}

// TestSystemRulesWithParserAppliesBotRule verifies that the hard-coded
// system rules (mirrored from Server.initSystemRules in
// pkg/api/server_not_enterprise.go) compile into non-stale matchers when
// a UA parser is provided, and that the "User-Agent bot detection" rule
// adjusts difficulty for bot User-Agents while leaving regular browsers
// untouched. This guards against regressions that re-introduce a nil
// parser in the system-rules compilation path.
func TestSystemRulesWithParserAppliesBotRule(t *testing.T) {
	compiled := NewRulesCompiler(useragent.NewParser()).Compile(context.Background(), systemRulesDBRules())
	if compiled == nil {
		t.Fatal("expected non-nil CompiledRules")
	}
	if compiled.IsStale() {
		t.Fatal("system rules compiled with a parser must not be stale")
	}

	const base int16 = 50
	wantBot := expectedDifficultyLevel(base, 300)

	for _, tc := range []struct {
		name      string
		ua        string
		wantLevel int16
	}{
		{"Curl", "curl/8.0.1", wantBot},
		{"Googlebot", "Googlebot/2.1 (+http://www.google.com/bot.html)", wantBot},
		{"EmptyUA", "", wantBot},
		{"RegularBrowser", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", base},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prop := stubProperty(base)
			ri := newCoreRequestInfo(tc.ua, netip.MustParseAddr("192.0.2.1"))
			applied, _ := compiled.Apply(ri, prop)
			if applied.Level() != tc.wantLevel {
				t.Errorf("UA=%q: level = %d, want %d", tc.ua, applied.Level(), tc.wantLevel)
			}
		})
	}
}

// TestSystemRulesClientSideDetectionWorksRegardlessOfParser ensures the
// "Client-side automation detection" system rule (which does not use the UA
// parser) keeps working under both nil and non-nil parser configurations,
// guarding against regressions from the parser wiring change.
func TestSystemRulesClientSideDetectionWorksRegardlessOfParser(t *testing.T) {
	assertAutomationLevel := func(t *testing.T, compiled *CompiledRules, hints, ua string, wantLevel int16) {
		t.Helper()
		ri := newTestRequestInfoWithHeaders(ua, netip.MustParseAddr("192.0.2.1"), map[string]string{
			common.HeaderClientHints: hints,
		})
		applied, _ := compiled.Apply(ri, stubProperty(50))
		if applied.Level() != wantLevel {
			t.Errorf("hints=%q UA=%q: level = %d, want %d", hints, ua, applied.Level(), wantLevel)
		}
	}

	wantAutomation := expectedDifficultyLevel(50, 300)

	t.Run("NilParser", func(t *testing.T) {
		compiled := NewRulesCompiler(nil).Compile(context.Background(), systemRulesDBRules())
		if compiled == nil {
			t.Fatal("expected non-nil CompiledRules")
		}
		assertAutomationLevel(t, compiled, "automation", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)", wantAutomation)
		assertAutomationLevel(t, compiled, "", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)", 50)
	})

	t.Run("WithParser", func(t *testing.T) {
		compiled := NewRulesCompiler(useragent.NewParser()).Compile(context.Background(), systemRulesDBRules())
		if compiled == nil {
			t.Fatal("expected non-nil CompiledRules")
		}
		assertAutomationLevel(t, compiled, "automation", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)", wantAutomation)
		assertAutomationLevel(t, compiled, "", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)", 50)
	})
}
