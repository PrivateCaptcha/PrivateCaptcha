package rules

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/medama-io/go-useragent"
)

var testCompiler = NewRulesCompiler(useragent.NewParser())

func newTestRequestInfo(userAgent string, ip netip.Addr) *RequestInfo {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", userAgent)
	ctx := context.WithValue(req.Context(), common.RateLimitKeyContextKey, ip)
	return NewRequestInfo(req.WithContext(ctx), "")
}

func newTestRequestInfoWithCountryCode(userAgent string, ip netip.Addr, headerName, countryCode string) *RequestInfo {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", userAgent)
	if len(countryCode) > 0 {
		req.Header.Set(headerName, countryCode)
	}
	ctx := context.WithValue(req.Context(), common.RateLimitKeyContextKey, ip)
	return NewRequestInfo(req.WithContext(ctx), headerName)
}

func newTestRequestInfoNoIP(userAgent string) *RequestInfo {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", userAgent)
	req.RemoteAddr = ""
	return NewRequestInfo(req, "")
}

func newTestRequestInfoWithDomain(userAgent string, ip netip.Addr, domain string) *RequestInfo {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", "https://"+domain)
	ctx := context.WithValue(req.Context(), common.RateLimitKeyContextKey, ip)
	return NewRequestInfo(req.WithContext(ctx), "")
}

func newTestRequestInfoWithHeaders(userAgent string, ip netip.Addr, headers map[string]string) *RequestInfo {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	ctx := context.WithValue(req.Context(), common.RateLimitKeyContextKey, ip)
	return NewRequestInfo(req.WithContext(ctx), "")
}

func newStubProperty() *difficulty.StubProperty {
	return difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthMedium)
}

func expectedDifficultyLevel(baseLevel int16, percentDiff int32) int16 {
	adjusted := baseLevel + int16(percentDiff*(common.DifficultyDelta/2)/100)
	if adjusted < int16(common.MinDifficultyLevel) {
		return int16(common.MinDifficultyLevel)
	} else if adjusted > int16(common.MaxDifficultyLevel) {
		return int16(common.MaxDifficultyLevel)
	}
	return adjusted
}

func applyRule(r rule, p difficulty.Property) *overrideProperty {
	op := &overrideProperty{base: p}
	r.Apply(op)
	return op
}

func TestUserAgentEqualsMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "BadBot/1.0", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))

	if !compiled.Matches(ri) {
		t.Error("Expected rule to match exact user agent")
	}

	ri2 := newTestRequestInfo("GoodBot/2.0", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match different user agent")
	}
}

func TestUserAgentContainsMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "BadBot", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	ri := newTestRequestInfo("Mozilla/5.0 BadBot/1.0", netip.MustParseAddr("1.2.3.4"))

	if !compiled.Matches(ri) {
		t.Error("Expected rule to match containing user agent")
	}

	ri2 := newTestRequestInfo("Mozilla/5.0", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match non-containing user agent")
	}
}

func TestUserAgentEmptyMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorEmpty,
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("", netip.MustParseAddr("1.2.3.4"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match empty user agent")
	}

	ri2 := newTestRequestInfo("Mozilla/5.0", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match non-empty user agent")
	}
}

func TestUserAgentInMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator:       dbgen.RuleConditionOperatorIn,
		ConditionValueStr:       pgtype.Text{String: "BadBot/1.0|GoodBot/2.0", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: "|", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match user agent in list")
	}

	ri2 := newTestRequestInfo("OtherBot/3.0", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match user agent not in list")
	}
}

func TestUserAgentBotMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorBot,
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	// Empty user agent counts as bot
	ri := newTestRequestInfo("", netip.MustParseAddr("1.2.3.4"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match empty user agent as bot")
	}

	// Known bot user agent should match
	ri2 := newTestRequestInfo("Googlebot/2.1 (+http://www.google.com/bot.html)", netip.MustParseAddr("1.2.3.4"))
	if !compiled.Matches(ri2) {
		t.Error("Expected rule to match known bot user agent")
	}

	// Regular browser should not match
	ri3 := newTestRequestInfo("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri3) {
		t.Error("Expected rule to not match regular browser user agent")
	}
}

func TestUserAgentBotNegatedMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator:        dbgen.RuleConditionOperatorBot,
		ConditionOperatorNegated: true,
		ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:              50,
		Enabled:                  true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	// Regular browser should match (negated)
	ri := newTestRequestInfo("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", netip.MustParseAddr("1.2.3.4"))
	if !compiled.Matches(ri) {
		t.Error("Expected negated bot rule to match regular browser user agent")
	}

	// Bot should not match (negated)
	ri2 := newTestRequestInfo("Googlebot/2.1 (+http://www.google.com/bot.html)", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri2) {
		t.Error("Expected negated bot rule to not match known bot user agent")
	}
}

func TestIPAddressMatchesPrefix(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       25,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("10.1.2.3"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match IP in prefix 10.0.0.0/8")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("192.168.1.1"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match IP outside prefix")
	}
}

func TestIPAddressMatchesExactAddr(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: pgtype.Text{String: "192.168.1.100", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
		ActionValue:       1,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("192.168.1.100"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match exact IP address")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("192.168.1.101"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match different IP")
	}
}

func TestIPAddressEmptyMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorEmpty,
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoNoIP("test")
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match when IP is not valid")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match when IP is valid")
	}
}

func TestDifficultyLevelApply(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       100, // +100% adds 8 difficulty levels
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty() // has level 50

	result := applyRule(compiled, prop)
	expected := expectedDifficultyLevel(50, 100)
	if result.Level() != expected {
		t.Errorf("Expected level %d, got %d", expected, result.Level())
	}
	if result.Growth() != dbgen.DifficultyGrowthMedium {
		t.Errorf("Expected growth to remain medium, got %s", result.Growth())
	}
}

func TestDifficultyLevelNegativePercentApply(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Mobile", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       -20, // -20%
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty() // has level 50

	result := applyRule(compiled, prop)
	expected := expectedDifficultyLevel(50, -20)
	if result.Level() != expected {
		t.Errorf("Expected level %d, got %d", expected, result.Level())
	}
}

func TestDifficultyLevelClampingLow(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Test", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       -300, // -300% subtracts 24 levels
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty() // has level 50

	result := applyRule(compiled, prop)
	expected := expectedDifficultyLevel(50, -300)
	if result.Level() != expected {
		t.Errorf("Expected level %d, got %d", expected, result.Level())
	}
}

func TestDifficultyLevelClampingHigh(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Test", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       1000, // +1000% clamped to +300%, adds 24 levels
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty() // has level 50

	result := applyRule(compiled, prop)
	// 1000 is clamped to 300 at compile time
	expected := expectedDifficultyLevel(50, 300)
	if result.Level() != expected {
		t.Errorf("Expected level %d, got %d", expected, result.Level())
	}
}

func TestDifficultyLevelPercentRanges(t *testing.T) {
	tests := []struct {
		name        string
		percentDiff int32
		baseLevel   int16
	}{
		{"plus 100 percent", 100, 50},
		{"plus 200 percent", 200, 50},
		{"plus 300 percent", 300, 50},
		{"minus 100 percent", -100, 50},
		{"minus 200 percent", -200, 50},
		{"minus 300 percent", -300, 50},
		{"plus 50 percent high base", 50, 100},
		{"minus 50 percent high base", -50, 100},
		{"plus 25 percent high base", 25, 100},
		{"small percent rounds toward zero", 10, 50},
		{"negative clamp to minimum", -300, 10},
		{"positive clamp to maximum", 300, 240},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &dbgen.DifficultyRule{
				ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator: dbgen.RuleConditionOperatorContains,
				ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
				ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:       tt.percentDiff,
				Enabled:           true,
			}

			compiled, err := testCompiler.CompileRule(context.Background(), rule)
			if err != nil {
				t.Fatal(err)
			}
			prop := difficulty.NewStubProperty(1, true, 1, 1, tt.baseLevel, dbgen.DifficultyGrowthMedium)

			ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
			op := &overrideProperty{base: prop}
			compiled.Matches(ri)
			compiled.Apply(op)

			expected := expectedDifficultyLevel(tt.baseLevel, tt.percentDiff)
			if op.Level() != expected {
				t.Errorf("Expected level %d, got %d", expected, op.Level())
			}
		})
	}
}

func TestDifficultyGrowthApply(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
		ActionValue:       3, // fast
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthSlow)

	result := applyRule(compiled, prop)
	if result.Growth() != dbgen.DifficultyGrowthFast {
		t.Errorf("Expected growth to be fast, got %s", result.Growth())
	}
	if result.Level() != 50 {
		t.Errorf("Expected level to remain 50, got %d", result.Level())
	}
}

func TestCompiledRulesApplyLastMatchWinsLevel(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Position:          1,
			Enabled:           true,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), propertyRules)
	prop := newStubProperty()

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	// Both rules match; last rule applies on top of first rule's result
	firstLevel := expectedDifficultyLevel(50, 50)
	expected := expectedDifficultyLevel(firstLevel, 100)
	if terminal {
		t.Error("Expected non-terminal result for level rules")
	}
	if result.Level() != expected {
		t.Errorf("Expected last-match-wins level %d, got %d", expected, result.Level())
	}
}

func TestRulesPairPropertyAndOrgCumulative(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			PropertyID:        pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       30,
			Position:          1,
			Enabled:           true,
		},
	}
	orgRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          1,
			Enabled:           true,
		},
	}

	compiledProp := testCompiler.Compile(context.Background(), propertyRules)
	compiledOrg := testCompiler.Compile(context.Background(), orgRules)
	rp := &RulesPair{PropertyRules: compiledProp, OrgRules: compiledOrg}
	prop := newStubProperty()

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := rp.Apply(ri, prop)
	// Org rule applies first, then property rule applies on top of the org rule result
	orgLevel := expectedDifficultyLevel(50, 100)
	expected := expectedDifficultyLevel(orgLevel, 30)
	if result.Level() != expected {
		t.Errorf("Expected cumulative level %d from org+property rules, got %d", expected, result.Level())
	}
}

func TestCompiledRulesNoMatch(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorEquals,
			ConditionValueStr: pgtype.Text{String: "SpecificBot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), propertyRules)
	prop := newStubProperty()

	ri := newTestRequestInfo("Mozilla/5.0", netip.MustParseAddr("1.2.3.4"))
	result, found := compiled.Apply(ri, prop)
	if found || result.Level() != 50 {
		t.Errorf("Expected original level 50 when no match, got %d", result.Level())
	}
}

func TestNilCompiledRulesApply(t *testing.T) {
	prop := newStubProperty()

	ri := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.4"))

	var cr *CompiledRules
	result, found := cr.Apply(ri, prop)
	if found || result.Level() != 50 {
		t.Errorf("Expected original property when compiled rules is nil, got level %d", result.Level())
	}

	if cr.IsRequestBlocked(ri) {
		t.Error("Expected nil compiled rules to not block request")
	}
}

func TestEmptyCompileReturnsNil(t *testing.T) {
	compiled := testCompiler.Compile(context.Background(), nil)
	if compiled != nil {
		t.Error("Expected nil CompiledRules from empty rule sets")
	}
}

func TestIsRequestBlocked(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator: dbgen.RuleConditionOperatorMatches,
			ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Terminal:          true,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), propertyRules)

	ri := newTestRequestInfo("test", netip.MustParseAddr("10.1.2.3"))
	if !compiled.IsRequestBlocked(ri) {
		t.Error("Expected request from 10.1.2.3 to be blocked")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("192.168.1.1"))
	if compiled.IsRequestBlocked(ri2) {
		t.Error("Expected request from 192.168.1.1 to not be blocked")
	}
}

func TestIsRequestBlockedChecksTypeFirst(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Position:          1,
			Enabled:           true,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator: dbgen.RuleConditionOperatorMatches,
			ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Terminal:          true,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), propertyRules)
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("10.1.2.3"))
	if !compiled.IsRequestBlocked(ri) {
		t.Error("Expected terminal block rule to still be checked even when non-terminal rule also matches")
	}
}

func TestIsRequestBlockedNoBlockRulesShortCircuit(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator: dbgen.RuleConditionOperatorMatches,
			ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), propertyRules)
	if compiled.hasBlockRules {
		t.Error("Expected hasBlockRules to be false when no block rules are present")
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("10.1.2.3"))
	if compiled.IsRequestBlocked(ri) {
		t.Error("Expected request to not be blocked when no block rules are present")
	}
}

func TestGrowthFromInt(t *testing.T) {
	tests := []struct {
		input    int32
		expected dbgen.DifficultyGrowth
	}{
		{0, dbgen.DifficultyGrowthConstant},
		{1, dbgen.DifficultyGrowthSlow},
		{2, dbgen.DifficultyGrowthMedium},
		{3, dbgen.DifficultyGrowthFast},
		{99, dbgen.DifficultyGrowthMedium},
	}

	for _, tt := range tests {
		result := growthFromInt(tt.input)
		if result != tt.expected {
			t.Errorf("growthFromInt(%d) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestIPv6PrefixMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: pgtype.Text{String: "2001:db8::/32", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("2001:db8::1"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match IPv6 address in prefix")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("2001:db9::1"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match IPv6 address outside prefix")
	}
}

func TestInvalidIPRuleValue(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: pgtype.Text{String: "not-an-ip", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err != ErrInvalidIPValue {
		t.Errorf("Expected ErrInvalidIPValue, got %v", err)
	}
}

func TestIPAddressMatchesMultiplePrefixes(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorMatches,
		ConditionValueStr:       pgtype.Text{String: "10.0.0.0/8,192.168.0.0/16", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             25,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri1 := newTestRequestInfo("test", netip.MustParseAddr("10.1.2.3"))
	if !compiled.Matches(ri1) {
		t.Error("Expected rule to match IP in first prefix 10.0.0.0/8")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("192.168.5.10"))
	if !compiled.Matches(ri2) {
		t.Error("Expected rule to match IP in second prefix 192.168.0.0/16")
	}

	ri3 := newTestRequestInfo("test", netip.MustParseAddr("172.16.0.1"))
	if compiled.Matches(ri3) {
		t.Error("Expected rule to not match IP outside all prefixes")
	}
}

func TestIPAddressMatchesMultipleAddrs(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorMatches,
		ConditionValueStr:       pgtype.Text{String: "1.2.3.4,5.6.7.8,9.10.11.12", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyHTTPRequest,
		ActionValue:             1,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	for _, ip := range []string{"1.2.3.4", "5.6.7.8", "9.10.11.12"} {
		ri := newTestRequestInfo("test", netip.MustParseAddr(ip))
		if !compiled.Matches(ri) {
			t.Errorf("Expected rule to match IP %s", ip)
		}
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.5"))
	if compiled.Matches(ri) {
		t.Error("Expected rule to not match unlisted IP")
	}
}

func TestIPAddressMatchesMixedPrefixAndAddr(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorMatches,
		ConditionValueStr:       pgtype.Text{String: "10.0.0.0/8,172.16.0.1", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri1 := newTestRequestInfo("test", netip.MustParseAddr("10.5.5.5"))
	if !compiled.Matches(ri1) {
		t.Error("Expected rule to match IP in prefix 10.0.0.0/8")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("172.16.0.1"))
	if !compiled.Matches(ri2) {
		t.Error("Expected rule to match exact address 172.16.0.1")
	}

	ri3 := newTestRequestInfo("test", netip.MustParseAddr("172.16.0.2"))
	if compiled.Matches(ri3) {
		t.Error("Expected rule to not match 172.16.0.2 (only 172.16.0.1 is listed)")
	}
}

func TestIPAddressMatchesMultipleNegated(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:        dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:        dbgen.RuleConditionOperatorMatches,
		ConditionOperatorNegated: true,
		ConditionValueStr:        pgtype.Text{String: "10.0.0.0/8,192.168.0.0/16", Valid: true},
		ConditionValueSeparator:  pgtype.Text{String: ",", Valid: true},
		ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:              50,
		Enabled:                  true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri1 := newTestRequestInfo("test", netip.MustParseAddr("10.1.2.3"))
	if compiled.Matches(ri1) {
		t.Error("Expected negated rule to not match IP in a listed prefix")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("172.16.0.1"))
	if !compiled.Matches(ri2) {
		t.Error("Expected negated rule to match IP outside all listed prefixes")
	}
}

func TestIPAddressInvalidInList(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorMatches,
		ConditionValueStr:       pgtype.Text{String: "10.0.0.0/8,not-valid", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err != ErrInvalidIPValue {
		t.Errorf("Expected ErrInvalidIPValue for invalid IP in list, got %v", err)
	}
}

func TestIPAddressSkipsEmptyItemsInList(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorMatches,
		ConditionValueStr:       pgtype.Text{String: "10.0.0.0/8,,192.168.1.1", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("Expected empty items to be skipped, got error: %v", err)
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("10.5.5.5"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match IP in valid prefix")
	}
}

func TestIPAddressAllEmptyItemsInListIsError(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorMatches,
		ConditionValueStr:       pgtype.Text{String: ",,,", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err != ErrInvalidIPValue {
		t.Errorf("Expected ErrInvalidIPValue for all-empty list, got %v", err)
	}
}

func TestIPv6AddressMatchesExact(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: pgtype.Text{String: "2001:db8::1", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("2001:db8::1"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match exact IPv6 address")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("2001:db8::2"))
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match different IPv6 address")
	}
}

func TestIPAddressMatchesMixedIPv4AndIPv6(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorMatches,
		ConditionValueStr:       pgtype.Text{String: "10.0.0.0/8,2001:db8::/32", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri1 := newTestRequestInfo("test", netip.MustParseAddr("10.1.2.3"))
	if !compiled.Matches(ri1) {
		t.Error("Expected rule to match IPv4 in 10.0.0.0/8")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("2001:db8::cafe"))
	if !compiled.Matches(ri2) {
		t.Error("Expected rule to match IPv6 in 2001:db8::/32")
	}

	ri3 := newTestRequestInfo("test", netip.MustParseAddr("192.168.1.1"))
	if compiled.Matches(ri3) {
		t.Error("Expected rule to not match IP outside all listed prefixes")
	}

	ri4 := newTestRequestInfo("test", netip.MustParseAddr("2001:db9::1"))
	if compiled.Matches(ri4) {
		t.Error("Expected rule to not match IPv6 outside listed prefix")
	}
}

func TestIPAddressEqualsExactMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "192.168.1.100", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("192.168.1.100"))
	if !compiled.Matches(ri) {
		t.Error("Expected equals rule to match exact IP address")
	}

	ri2 := newTestRequestInfo("test", netip.MustParseAddr("192.168.1.101"))
	if compiled.Matches(ri2) {
		t.Error("Expected equals rule to not match different IP")
	}
}

func TestIPAddressEqualsRejectsCIDR(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err != ErrIPNonSingletonPrefix {
		t.Errorf("Expected ErrIPNonSingletonPrefix for CIDR in equals, got %v", err)
	}
}

func TestIPAddressEqualsRejectsMultipleValues(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorEquals,
		ConditionValueStr:       pgtype.Text{String: "1.2.3.4,5.6.7.8", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err != ErrIPEqualsMultipleValues {
		t.Errorf("Expected ErrIPEqualsMultipleValues for multiple IPs in equals, got %v", err)
	}
}

func TestIPAddressInSingleIPMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorIn,
		ConditionValueStr:       pgtype.Text{String: "1.2.3.4,5.6.7.8,9.10.11.12", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	for _, ip := range []string{"1.2.3.4", "5.6.7.8", "9.10.11.12"} {
		ri := newTestRequestInfo("test", netip.MustParseAddr(ip))
		if !compiled.Matches(ri) {
			t.Errorf("Expected in rule to match listed IP %s", ip)
		}
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.5"))
	if compiled.Matches(ri) {
		t.Error("Expected in rule to not match unlisted IP")
	}
}

func TestIPAddressInRejectsCIDR(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator:       dbgen.RuleConditionOperatorIn,
		ConditionValueStr:       pgtype.Text{String: "10.0.0.0/8,1.2.3.4", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err != ErrIPNonSingletonPrefix {
		t.Errorf("Expected ErrIPNonSingletonPrefix for CIDR in 'in' operator, got %v", err)
	}
}

func TestUnknownConditionProperty(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: "unknown_prop",
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "test", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err != ErrUnknownConditionProperty {
		t.Errorf("Expected ErrUnknownConditionProperty, got %v", err)
	}
}

func TestUnknownActionProperty(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "test", Valid: true},
		ActionProperty:    "unknown_action",
		ActionValue:       50,
		Enabled:           true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err != ErrUnknownActionProperty {
		t.Errorf("Expected ErrUnknownActionProperty, got %v", err)
	}
}

func TestCompileSkipsInvalidAndDisabledRules(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: "unknown_prop",
			ConditionOperator: dbgen.RuleConditionOperatorEquals,
			ConditionValueStr: pgtype.Text{String: "test", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Enabled:           true,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       199,
			Enabled:           false,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       0,
			Enabled:           true,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       25,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), propertyRules)
	if compiled == nil {
		t.Fatal("Expected non-nil CompiledRules (one valid rule)")
	}

	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	expected := expectedDifficultyLevel(50, 25)
	if terminal {
		t.Error("Expected non-terminal result for level rule")
	}
	if result.Level() != expected {
		t.Errorf("Expected level %d from valid rule, got %d", expected, result.Level())
	}
}

func TestOverridePropertyPreservesBase(t *testing.T) {
	base := difficulty.NewStubProperty(42, true, 10, 5, 80, dbgen.DifficultyGrowthSlow)
	level := int16(150)
	op := &overrideProperty{base: base, level: &level}

	if op.ID() != 42 {
		t.Errorf("Expected ID 42, got %d", op.ID())
	}
	if !op.Valid() {
		t.Error("Expected Valid() to be true")
	}
	if op.OwnerID() != 10 {
		t.Errorf("Expected OwnerID 10, got %d", op.OwnerID())
	}
	if op.OrgID() != 5 {
		t.Errorf("Expected OrgID 5, got %d", op.OrgID())
	}
	if op.Level() != 150 {
		t.Errorf("Expected Level 150, got %d", op.Level())
	}
	if op.Growth() != dbgen.DifficultyGrowthSlow {
		t.Errorf("Expected Growth slow, got %s", op.Growth())
	}
}

func TestCountryCodeEqualsMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyCountryCode,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "US", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "US")
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match country code US")
	}

	ri2 := newTestRequestInfoWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "DE")
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match country code DE")
	}
}

func TestCountryCodeNoHeader(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyCountryCode,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "US", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri) {
		t.Error("Expected rule to not match when no country code header configured")
	}
}

func TestCountryCodeContainsMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyCountryCode,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "u", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "US")
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match country code containing 'u' (case-insensitive)")
	}

	ri2 := newTestRequestInfoWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "DE")
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match country code DE for contains 'u'")
	}
}

func TestCountryCodeInMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyCountryCode,
		ConditionOperator: dbgen.RuleConditionOperatorIn,
		ConditionValueStr: pgtype.Text{String: "US,GB,CA", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "GB")
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match country code in list")
	}

	ri2 := newTestRequestInfoWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "DE")
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match country code not in list")
	}
}

func TestCountryCodeEmptyMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyCountryCode,
		ConditionOperator: dbgen.RuleConditionOperatorEmpty,
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.4"))
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match when country code is empty")
	}

	ri2 := newTestRequestInfoWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "US")
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match when country code is present")
	}
}

func TestRequestInfoLazyCaching(t *testing.T) {
	ri := newTestRequestInfoWithCountryCode("TestBot/1.0", netip.MustParseAddr("10.0.0.1"), "X-Country", "US")

	ua := ri.UserAgent()
	if ua != "TestBot/1.0" {
		t.Errorf("Expected UserAgent 'TestBot/1.0', got %q", ua)
	}

	if ri.UserAgent() != ua {
		t.Error("Expected same cached user agent")
	}

	ip := ri.IPAddr()
	if ip != netip.MustParseAddr("10.0.0.1") {
		t.Errorf("Expected IP 10.0.0.1, got %v", ip)
	}

	cc := ri.CountryCode()
	if cc != "US" {
		t.Errorf("Expected country code 'US', got %q", cc)
	}
}

func TestRequestInfoNoCountryCodeHeader(t *testing.T) {
	ri := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.4"))
	cc := ri.CountryCode()
	if cc != "" {
		t.Errorf("Expected empty country code, got %q", cc)
	}
}

func TestRequestInfoIPAddrFallback(t *testing.T) {
	ri := newTestRequestInfoNoIP("test")
	ip := ri.IPAddr()
	if ip.IsValid() {
		t.Error("Expected invalid IP when no context IP and httptest default remote addr")
	}
}

func TestNilRulesPairApply(t *testing.T) {
	prop := newStubProperty()
	ri := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.4"))

	var rp *RulesPair
	result := rp.Apply(ri, prop)
	if result.Level() != 50 {
		t.Errorf("Expected original property when rules pair is nil, got level %d", result.Level())
	}

	if rp.IsRequestBlocked(ri) {
		t.Error("Expected nil rules pair to not block request")
	}
}

func TestRulesPairOrgFallback(t *testing.T) {
	orgRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Enabled:           true,
		},
	}
	org := testCompiler.Compile(context.Background(), orgRules)
	rp := &RulesPair{OrgRules: org}

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	prop := newStubProperty()
	result := rp.Apply(ri, prop)
	expected := expectedDifficultyLevel(50, 100)
	if result.Level() != expected {
		t.Errorf("Expected org rule (%d) when no property rules, got %d", expected, result.Level())
	}
}

func TestRulesPairPropertyOnly(t *testing.T) {
	propRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Enabled:           true,
		},
	}
	prop := testCompiler.Compile(context.Background(), propRules)
	rp := &RulesPair{PropertyRules: prop}

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	p := newStubProperty()
	result := rp.Apply(ri, p)
	expected := expectedDifficultyLevel(50, 50)
	if result.Level() != expected {
		t.Errorf("Expected property rule level %d when no org rules, got %d", expected, result.Level())
	}
}

// Tests for condition negation

func TestUserAgentNegation(t *testing.T) {
	tests := []struct {
		name      string
		operator  dbgen.RuleConditionOperator
		value     string
		separator string
		userAgent string
		negated   bool
		wantMatch bool
	}{
		{
			name:      "equals negated matches different",
			operator:  dbgen.RuleConditionOperatorEquals,
			value:     "BadBot/1.0",
			userAgent: "GoodBot/2.0",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "equals negated not matches same",
			operator:  dbgen.RuleConditionOperatorEquals,
			value:     "BadBot/1.0",
			userAgent: "BadBot/1.0",
			negated:   true,
			wantMatch: false,
		},
		{
			name:      "contains negated matches non-containing",
			operator:  dbgen.RuleConditionOperatorContains,
			value:     "BadBot",
			userAgent: "Mozilla/5.0",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "contains negated not matches containing",
			operator:  dbgen.RuleConditionOperatorContains,
			value:     "BadBot",
			userAgent: "Mozilla/5.0 BadBot/1.0",
			negated:   true,
			wantMatch: false,
		},
		{
			name:      "empty negated matches non-empty",
			operator:  dbgen.RuleConditionOperatorEmpty,
			userAgent: "Mozilla/5.0",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "empty negated not matches empty",
			operator:  dbgen.RuleConditionOperatorEmpty,
			userAgent: "",
			negated:   true,
			wantMatch: false,
		},
		{
			name:      "in negated matches not in list",
			operator:  dbgen.RuleConditionOperatorIn,
			value:     "BadBot/1.0|EvilBot/2.0",
			separator: "|",
			userAgent: "GoodBot/3.0",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "in negated not matches in list",
			operator:  dbgen.RuleConditionOperatorIn,
			value:     "BadBot/1.0|EvilBot/2.0",
			separator: "|",
			userAgent: "BadBot/1.0",
			negated:   true,
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &dbgen.DifficultyRule{
				ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator:        tt.operator,
				ConditionOperatorNegated: tt.negated,
				ConditionValueStr:        pgtype.Text{String: tt.value, Valid: true},
				ConditionValueSeparator:  pgtype.Text{String: tt.separator, Valid: len(tt.separator) > 0},
				ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:              50,
				Enabled:                  true,
			}

			compiled, err := testCompiler.CompileRule(context.Background(), rule)
			if err != nil {
				t.Fatal(err)
			}

			ri := newTestRequestInfo(tt.userAgent, netip.MustParseAddr("1.2.3.4"))
			gotMatch := compiled.Matches(ri)

			if gotMatch != tt.wantMatch {
				t.Errorf("Matches() = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}

func TestIPAddressNegation(t *testing.T) {
	tests := []struct {
		name      string
		operator  dbgen.RuleConditionOperator
		value     string
		ip        string
		negated   bool
		wantMatch bool
	}{
		{
			name:      "matches negated matches outside prefix",
			operator:  dbgen.RuleConditionOperatorMatches,
			value:     "10.0.0.0/8",
			ip:        "192.168.1.1",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "matches negated not matches inside prefix",
			operator:  dbgen.RuleConditionOperatorMatches,
			value:     "10.0.0.0/8",
			ip:        "10.1.2.3",
			negated:   true,
			wantMatch: false,
		},
		{
			name:      "matches exact IP negated matches different IP",
			operator:  dbgen.RuleConditionOperatorMatches,
			value:     "192.168.1.100",
			ip:        "192.168.1.101",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "matches exact IP negated not matches same IP",
			operator:  dbgen.RuleConditionOperatorMatches,
			value:     "192.168.1.100",
			ip:        "192.168.1.100",
			negated:   true,
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &dbgen.DifficultyRule{
				ConditionProperty:        dbgen.RuleConditionPropertyIPAddress,
				ConditionOperator:        tt.operator,
				ConditionOperatorNegated: tt.negated,
				ConditionValueStr:        pgtype.Text{String: tt.value, Valid: true},
				ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:              50,
				Enabled:                  true,
			}

			compiled, err := testCompiler.CompileRule(context.Background(), rule)
			if err != nil {
				t.Fatal(err)
			}

			ri := newTestRequestInfo("test", netip.MustParseAddr(tt.ip))
			gotMatch := compiled.Matches(ri)

			if gotMatch != tt.wantMatch {
				t.Errorf("Matches() = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}

func TestIPAddressEmptyNegation(t *testing.T) {
	tests := []struct {
		name      string
		hasIP     bool
		negated   bool
		wantMatch bool
	}{
		{
			name:      "empty negated matches valid IP",
			hasIP:     true,
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "empty negated not matches invalid IP",
			hasIP:     false,
			negated:   true,
			wantMatch: false,
		},
		{
			name:      "empty not negated matches invalid IP",
			hasIP:     false,
			negated:   false,
			wantMatch: true,
		},
		{
			name:      "empty not negated not matches valid IP",
			hasIP:     true,
			negated:   false,
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &dbgen.DifficultyRule{
				ConditionProperty:        dbgen.RuleConditionPropertyIPAddress,
				ConditionOperator:        dbgen.RuleConditionOperatorEmpty,
				ConditionOperatorNegated: tt.negated,
				ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:              50,
				Enabled:                  true,
			}

			compiled, err := testCompiler.CompileRule(context.Background(), rule)
			if err != nil {
				t.Fatal(err)
			}

			var ri *RequestInfo
			if tt.hasIP {
				ri = newTestRequestInfo("test", netip.MustParseAddr("1.2.3.4"))
			} else {
				ri = newTestRequestInfoNoIP("test")
			}

			gotMatch := compiled.Matches(ri)

			if gotMatch != tt.wantMatch {
				t.Errorf("Matches() = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}

func TestCountryCodeNegation(t *testing.T) {
	tests := []struct {
		name        string
		operator    dbgen.RuleConditionOperator
		value       string
		separator   string
		countryCode string
		negated     bool
		wantMatch   bool
	}{
		{
			name:        "equals negated matches different",
			operator:    dbgen.RuleConditionOperatorEquals,
			value:       "US",
			countryCode: "CA",
			negated:     true,
			wantMatch:   true,
		},
		{
			name:        "equals negated not matches same",
			operator:    dbgen.RuleConditionOperatorEquals,
			value:       "US",
			countryCode: "US",
			negated:     true,
			wantMatch:   false,
		},
		{
			name:        "contains negated matches non-containing",
			operator:    dbgen.RuleConditionOperatorContains,
			value:       "US",
			countryCode: "CA",
			negated:     true,
			wantMatch:   true,
		},
		{
			name:        "contains negated not matches containing",
			operator:    dbgen.RuleConditionOperatorContains,
			value:       "US",
			countryCode: "US",
			negated:     true,
			wantMatch:   false,
		},
		{
			name:        "empty negated matches non-empty",
			operator:    dbgen.RuleConditionOperatorEmpty,
			countryCode: "US",
			negated:     true,
			wantMatch:   true,
		},
		{
			name:        "empty negated not matches empty",
			operator:    dbgen.RuleConditionOperatorEmpty,
			countryCode: "",
			negated:     true,
			wantMatch:   false,
		},
		{
			name:        "in negated matches not in list",
			operator:    dbgen.RuleConditionOperatorIn,
			value:       "US,CA,MX",
			separator:   ",",
			countryCode: "UK",
			negated:     true,
			wantMatch:   true,
		},
		{
			name:        "in negated not matches in list",
			operator:    dbgen.RuleConditionOperatorIn,
			value:       "US,CA,MX",
			separator:   ",",
			countryCode: "CA",
			negated:     true,
			wantMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &dbgen.DifficultyRule{
				ConditionProperty:        dbgen.RuleConditionPropertyCountryCode,
				ConditionOperator:        tt.operator,
				ConditionOperatorNegated: tt.negated,
				ConditionValueStr:        pgtype.Text{String: tt.value, Valid: true},
				ConditionValueSeparator:  pgtype.Text{String: tt.separator, Valid: len(tt.separator) > 0},
				ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:              50,
				Enabled:                  true,
			}

			compiled, err := testCompiler.CompileRule(context.Background(), rule)
			if err != nil {
				t.Fatal(err)
			}

			ri := newTestRequestInfoWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "CF-IPCountry", tt.countryCode)
			gotMatch := compiled.Matches(ri)

			if gotMatch != tt.wantMatch {
				t.Errorf("Matches() = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}

func TestDomainEqualsMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyDomain,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "example.com", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	ri := newTestRequestInfoWithDomain("test", netip.MustParseAddr("1.2.3.4"), "example.com")

	if !compiled.Matches(ri) {
		t.Error("Expected rule to match exact domain")
	}

	ri2 := newTestRequestInfoWithDomain("test", netip.MustParseAddr("1.2.3.4"), "other.com")
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match different domain")
	}
}

func TestDomainContainsMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyDomain,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "example", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	ri := newTestRequestInfoWithDomain("test", netip.MustParseAddr("1.2.3.4"), "test.example.com")

	if !compiled.Matches(ri) {
		t.Error("Expected rule to match containing domain")
	}

	ri2 := newTestRequestInfoWithDomain("test", netip.MustParseAddr("1.2.3.4"), "other.com")
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match non-containing domain")
	}
}

func TestDomainEmptyMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyDomain,
		ConditionOperator: dbgen.RuleConditionOperatorEmpty,
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	// Request with empty domain (no Origin or Referer headers)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "test")
	ctx := context.WithValue(req.Context(), common.RateLimitKeyContextKey, netip.MustParseAddr("1.2.3.4"))
	ri := NewRequestInfo(req.WithContext(ctx), "")

	if !compiled.Matches(ri) {
		t.Error("Expected rule to match empty domain")
	}

	// Request with non-empty domain
	ri2 := newTestRequestInfoWithDomain("test", netip.MustParseAddr("1.2.3.4"), "example.com")
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match non-empty domain")
	}
}

func TestDomainNegation(t *testing.T) {
	tests := []struct {
		name      string
		operator  dbgen.RuleConditionOperator
		value     string
		domain    string
		negated   bool
		wantMatch bool
	}{
		{
			name:      "equals negated matches different",
			operator:  dbgen.RuleConditionOperatorEquals,
			value:     "example.com",
			domain:    "other.com",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "equals negated not matches same",
			operator:  dbgen.RuleConditionOperatorEquals,
			value:     "example.com",
			domain:    "example.com",
			negated:   true,
			wantMatch: false,
		},
		{
			name:      "contains negated matches non-containing",
			operator:  dbgen.RuleConditionOperatorContains,
			value:     "example",
			domain:    "other.com",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "contains negated not matches containing",
			operator:  dbgen.RuleConditionOperatorContains,
			value:     "example",
			domain:    "test.example.com",
			negated:   true,
			wantMatch: false,
		},
		{
			name:      "empty negated matches non-empty",
			operator:  dbgen.RuleConditionOperatorEmpty,
			domain:    "example.com",
			negated:   true,
			wantMatch: true,
		},
		{
			name:      "empty negated not matches empty",
			operator:  dbgen.RuleConditionOperatorEmpty,
			domain:    "",
			negated:   true,
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &dbgen.DifficultyRule{
				ConditionProperty:        dbgen.RuleConditionPropertyDomain,
				ConditionOperator:        tt.operator,
				ConditionOperatorNegated: tt.negated,
				ConditionValueStr:        pgtype.Text{String: tt.value, Valid: true},
				ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:              50,
				Enabled:                  true,
			}

			compiled, err := testCompiler.CompileRule(context.Background(), rule)
			if err != nil {
				t.Fatal(err)
			}

			var ri *RequestInfo
			if tt.domain == "" {
				// Create request without Origin/Referer headers for empty domain
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("User-Agent", "test")
				ctx := context.WithValue(req.Context(), common.RateLimitKeyContextKey, netip.MustParseAddr("1.2.3.4"))
				ri = NewRequestInfo(req.WithContext(ctx), "")
			} else {
				ri = newTestRequestInfoWithDomain("test", netip.MustParseAddr("1.2.3.4"), tt.domain)
			}
			gotMatch := compiled.Matches(ri)

			if gotMatch != tt.wantMatch {
				t.Errorf("Matches() = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}

// customMatcher is a wrapper around StringMatcher that extracts a custom field.
// This illustrates how external packages can wrap StringMatcher to add new condition properties.
type customMatcher struct {
	StringMatcher
}

func (m *customMatcher) Matches(ri *RequestInfo) bool {
	// custom extraction: use user agent as the value to match against
	extracted := ri.UserAgent()
	switch m.ConditionOperator {
	case dbgen.RuleConditionOperatorEquals:
		return strings.EqualFold(extracted, m.ConditionValueStr)
	default:
		return false
	}
}

func TestRegisterMatcherFactory(t *testing.T) {
	const testCustomPropertyName = "custom_property"

	compiler := NewRulesCompiler(useragent.NewParser())

	compiler.RegisterMatcherFactory(testCustomPropertyName, func(rule *dbgen.DifficultyRule) (Matcher, error) {
		return &customMatcher{
			StringMatcher: StringMatcher{
				ConditionOperator: rule.ConditionOperator,
				ConditionValueStr: rule.ConditionValueStr.String,
			},
		}, nil
	})

	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionProperty(testCustomPropertyName),
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "CustomAgent/1.0", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       10,
		Enabled:           true,
	}

	compiled, err := compiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("CompileRule() with custom factory failed: %v", err)
	}

	ri := newTestRequestInfo("CustomAgent/1.0", netip.MustParseAddr("1.2.3.4"))
	if !compiled.Matches(ri) {
		t.Error("Expected custom factory rule to match")
	}

	ri2 := newTestRequestInfo("OtherAgent/2.0", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ri2) {
		t.Error("Expected custom factory rule to not match different agent")
	}
}

func TestUnknownConditionPropertyReturnsError(t *testing.T) {
	compiler := NewRulesCompiler(useragent.NewParser())

	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionProperty("unknown_property"),
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "value", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       10,
		Enabled:           true,
	}

	_, err := compiler.CompileRule(context.Background(), rule)
	if err == nil {
		t.Error("Expected error for unknown condition property, got nil")
	}
}

func TestBuildStringMatcherExported(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "TestAgent/1.0", Valid: true},
	}

	m, err := BuildStringMatcher(rule)
	if err != nil {
		t.Fatalf("BuildStringMatcher() failed: %v", err)
	}

	ri := newTestRequestInfo("TestAgent/1.0", netip.MustParseAddr("1.2.3.4"))
	if !m.Matches(ri) {
		t.Error("Expected BuildStringMatcher result to match")
	}
}

func TestBuildIPMatcherExported(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
	}

	m, err := BuildIPMatcher(rule)
	if err != nil {
		t.Fatalf("BuildIPMatcher() failed: %v", err)
	}

	ri := newTestRequestInfo("agent", netip.MustParseAddr("10.1.2.3"))
	if !m.Matches(ri) {
		t.Error("Expected BuildIPMatcher result to match IP in range")
	}

	ri2 := newTestRequestInfo("agent", netip.MustParseAddr("192.168.1.1"))
	if m.Matches(ri2) {
		t.Error("Expected BuildIPMatcher result to not match IP out of range")
	}
}

func TestBreakRuleApply(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ID:                1,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyBreak,
		Terminal:          true,
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	if !compiled.Matches(ri) {
		t.Error("Expected break rule to match")
	}

	prop := newStubProperty()
	result := applyRule(compiled, prop)
	if result.Level() != prop.Level() {
		t.Errorf("Expected break rule to preserve level %d, got %d", prop.Level(), result.Level())
	}
	if result.Growth() != prop.Growth() {
		t.Errorf("Expected break rule to preserve growth %v, got %v", prop.Growth(), result.Growth())
	}
	if result.RuleID() != 1 {
		t.Errorf("Expected break rule to set RuleID to 1, got %d", result.RuleID())
	}
}

func TestOrgRuleRunsBeforePropertyBreakRule(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			PropertyID:        pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyBreak,
			Terminal:          true,
			Enabled:           true,
		},
	}
	orgRules := []*dbgen.DifficultyRule{
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Enabled:           true,
		},
	}

	compiledProp := testCompiler.Compile(context.Background(), propertyRules)
	compiledOrg := testCompiler.Compile(context.Background(), orgRules)
	rp := &RulesPair{PropertyRules: compiledProp, OrgRules: compiledOrg}
	prop := newStubProperty()

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := rp.Apply(ri, prop)
	// Org rule applies first (+100%), then property break rule fires but nothing follows it
	expected := expectedDifficultyLevel(50, 100)
	if result.Level() != expected {
		t.Errorf("Expected org rule level %d to apply before property break rule, got %d", expected, result.Level())
	}
	if result.RuleID() != 1 {
		t.Errorf("Expected property break rule RuleID 1, got %d", result.RuleID())
	}
}

func TestBreakRuleInOrgStopsProcessing(t *testing.T) {
	orgRules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyBreak,
			Terminal:          true,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          2,
			Enabled:           true,
		},
	}

	compiledOrg := testCompiler.Compile(context.Background(), orgRules)
	rp := &RulesPair{OrgRules: compiledOrg}
	prop := newStubProperty()

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := rp.Apply(ri, prop)
	// Break rule matched first, so second rule should NOT apply
	if result.Level() != 50 {
		t.Errorf("Expected break rule to preserve original level 50, got %d", result.Level())
	}
	if result.RuleID() != 1 {
		t.Errorf("Expected break rule RuleID 1, got %d", result.RuleID())
	}
}

func TestBreakRuleNoMatchFallsThrough(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorEquals,
			ConditionValueStr: pgtype.Text{String: "SpecificBot", Valid: true},
			PropertyID:        pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyBreak,
			Terminal:          true,
			Enabled:           true,
		},
	}
	orgRules := []*dbgen.DifficultyRule{
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Enabled:           true,
		},
	}

	compiledProp := testCompiler.Compile(context.Background(), propertyRules)
	compiledOrg := testCompiler.Compile(context.Background(), orgRules)
	rp := &RulesPair{PropertyRules: compiledProp, OrgRules: compiledOrg}
	prop := newStubProperty()

	ri := newTestRequestInfo("OtherBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := rp.Apply(ri, prop)
	// Break rule does NOT match, so org rule SHOULD apply
	expected := expectedDifficultyLevel(50, 100)
	if result.Level() != expected {
		t.Errorf("Expected org rule to apply when break rule does not match, got level %d", result.Level())
	}
}

// Terminal logic tests

func TestTerminalLevelRuleStopsProcessing(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Terminal:          true,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if !terminal {
		t.Error("Expected terminal result")
	}
	expected := expectedDifficultyLevel(50, 50)
	if result.Level() != expected {
		t.Errorf("Expected level %d from terminal rule, got %d", expected, result.Level())
	}
}

func TestNonTerminalLevelRulesCumulative(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       20,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if terminal {
		t.Error("Expected non-terminal result")
	}
	firstLevel := expectedDifficultyLevel(50, 20)
	expected := expectedDifficultyLevel(firstLevel, 50)
	if result.Level() != expected {
		t.Errorf("Expected last-match-wins level %d, got %d", expected, result.Level())
	}
}

func TestNonTerminalGrowthRulesCumulativeMax(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
			ActionValue:       1,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
			ActionValue:       3,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthConstant)
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if terminal {
		t.Error("Expected non-terminal result")
	}
	if result.Growth() != dbgen.DifficultyGrowthFast {
		t.Errorf("Expected max growth fast, got %s", result.Growth())
	}
}

func TestNonTerminalGrowthRulesLastMatchWins(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
			ActionValue:       3,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
			ActionValue:       1,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthConstant)
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if terminal {
		t.Error("Expected non-terminal result")
	}
	if result.Growth() != dbgen.DifficultyGrowthSlow {
		t.Errorf("Expected last-match-wins growth slow, got %s", result.Growth())
	}
}

func TestMixedLevelAndGrowthCumulative(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
			ActionValue:       3,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthSlow)
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if terminal {
		t.Error("Expected non-terminal result")
	}
	expected := expectedDifficultyLevel(50, 50)
	if result.Level() != expected {
		t.Errorf("Expected level %d, got %d", expected, result.Level())
	}
	if result.Growth() != dbgen.DifficultyGrowthFast {
		t.Errorf("Expected growth fast, got %s", result.Growth())
	}
}

func TestTerminalBreakStopsLevelAccumulation(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       20,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyBreak,
			Terminal:          true,
			Position:          2,
			Enabled:           true,
		},
		{
			ID:                3,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          3,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if !terminal {
		t.Error("Expected terminal result from break rule")
	}
	expected := expectedDifficultyLevel(50, 20)
	if result.Level() != expected {
		t.Errorf("Expected level %d (only first level rule applied before break), got %d", expected, result.Level())
	}
}

func TestTerminalBlockRequestStopsProcessing(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator: dbgen.RuleConditionOperatorMatches,
			ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Terminal:          true,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("10.1.2.3"))
	result, terminal := compiled.Apply(ri, prop)
	if !terminal {
		t.Error("Expected terminal result from block rule")
	}
	if result.Level() != 50 {
		t.Errorf("Expected original level 50 after terminal block, got %d", result.Level())
	}
}

func TestTerminalOrgRuleStopsPropertyRules(t *testing.T) {
	orgRules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Terminal:          true,
			Position:          1,
			Enabled:           true,
		},
	}
	propertyRules := []*dbgen.DifficultyRule{
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			PropertyID:        pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       30,
			Position:          1,
			Enabled:           true,
		},
	}

	compiledOrg := testCompiler.Compile(context.Background(), orgRules)
	compiledProp := testCompiler.Compile(context.Background(), propertyRules)
	rp := &RulesPair{PropertyRules: compiledProp, OrgRules: compiledOrg}
	prop := newStubProperty()

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := rp.Apply(ri, prop)
	expected := expectedDifficultyLevel(50, 100)
	if result.Level() != expected {
		t.Errorf("Expected level %d from terminal org rule only, got %d", expected, result.Level())
	}
}

func TestNonTerminalOrgRuleAllowsPropertyRules(t *testing.T) {
	orgRules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
			ActionValue:       1,
			Position:          1,
			Enabled:           true,
		},
	}
	propertyRules := []*dbgen.DifficultyRule{
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			PropertyID:        pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
			ActionValue:       3,
			Position:          1,
			Enabled:           true,
		},
	}

	compiledOrg := testCompiler.Compile(context.Background(), orgRules)
	compiledProp := testCompiler.Compile(context.Background(), propertyRules)
	rp := &RulesPair{PropertyRules: compiledProp, OrgRules: compiledOrg}
	prop := difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthConstant)

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := rp.Apply(ri, prop)
	if result.Growth() != dbgen.DifficultyGrowthFast {
		t.Errorf("Expected growth fast from property rule, got %s", result.Growth())
	}
}

func TestTerminalLevelRuleInMiddle(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       20,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Terminal:          true,
			Position:          2,
			Enabled:           true,
		},
		{
			ID:                3,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          3,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if !terminal {
		t.Error("Expected terminal result")
	}
	// Rule 1 (+20%) applies first, then rule 2 (+50% terminal) applies on top; rule 3 (+100%) not reached
	firstLevel := expectedDifficultyLevel(50, 20)
	expected := expectedDifficultyLevel(firstLevel, 50)
	if result.Level() != expected {
		t.Errorf("Expected level %d (last-match-wins before terminal stop), got %d", expected, result.Level())
	}
}

func TestBlockRuleTerminalWithPriorLevel(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator: dbgen.RuleConditionOperatorMatches,
			ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Terminal:          true,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("10.1.2.3"))
	result, terminal := compiled.Apply(ri, prop)
	if !terminal {
		t.Error("Expected terminal result from block rule")
	}
	expected := expectedDifficultyLevel(50, 50)
	if result.Level() != expected {
		t.Errorf("Expected level %d (accumulated before block), got %d", expected, result.Level())
	}
}

func TestMultipleNonTerminalLastMatchWinsLevel(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       20,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if terminal {
		t.Error("Expected non-terminal result")
	}
	firstLevel := expectedDifficultyLevel(50, 100)
	expected := expectedDifficultyLevel(firstLevel, 20)
	if result.Level() != expected {
		t.Errorf("Expected last-match-wins level %d, got %d", expected, result.Level())
	}
}

func TestOnlySecondRuleMatches(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorEquals,
			ConditionValueStr: pgtype.Text{String: "SpecificBot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := newStubProperty()
	ri := newTestRequestInfo("OtherBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if terminal {
		t.Error("Expected non-terminal result")
	}
	expected := expectedDifficultyLevel(50, 50)
	if result.Level() != expected {
		t.Errorf("Expected level %d from second rule only, got %d", expected, result.Level())
	}
}

func TestTerminalBreakRulePreservesAccumulatedGrowth(t *testing.T) {
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
			ActionValue:       3,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyBreak,
			Terminal:          true,
			Position:          2,
			Enabled:           true,
		},
		{
			ID:                3,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          3,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	prop := difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthSlow)
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result, terminal := compiled.Apply(ri, prop)
	if !terminal {
		t.Error("Expected terminal result")
	}
	if result.Growth() != dbgen.DifficultyGrowthFast {
		t.Errorf("Expected accumulated growth fast, got %s", result.Growth())
	}
	if result.Level() != 50 {
		t.Errorf("Expected original level 50, got %d", result.Level())
	}
}

func TestBlockRuleAlwaysTerminalEvenIfNotSetInDB(t *testing.T) {
	// Block rules always have terminal forced to true by the compiler,
	// regardless of the Terminal field in the DB. So they always block.
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator: dbgen.RuleConditionOperatorMatches,
			ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Terminal:          false,
			Position:          1,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	ri := newTestRequestInfo("test", netip.MustParseAddr("10.1.2.3"))
	if !compiled.IsRequestBlocked(ri) {
		t.Error("Expected block rule to always block regardless of Terminal field in DB")
	}
}

func TestOrgBreakRuleStopsPropertyRules(t *testing.T) {
	// Break rules always have terminal forced to true by the compiler,
	// regardless of the Terminal field in the DB. So they always stop property rules.
	orgRules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyBreak,
			// Terminal not set, defaults to false in Go, but compiler forces it to true
			Enabled: true,
		},
	}
	propertyRules := []*dbgen.DifficultyRule{
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			PropertyID:        pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Enabled:           true,
		},
	}

	compiledOrg := testCompiler.Compile(context.Background(), orgRules)
	compiledProp := testCompiler.Compile(context.Background(), propertyRules)
	rp := &RulesPair{PropertyRules: compiledProp, OrgRules: compiledOrg}
	prop := newStubProperty()

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := rp.Apply(ri, prop)
	// Org break rule always stops property rules (terminal is forced to true by compiler)
	if result.Level() != prop.Level() {
		t.Errorf("Expected property rule to be skipped when org break rule matches, got level %d", result.Level())
	}
}

func TestHTTPHeaderNameInSingleMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyHTTPHeaderName,
		ConditionOperator:       dbgen.RuleConditionOperatorIn,
		ConditionValueStr:       pgtype.Text{String: "X-Custom-Header", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{"X-Custom-Header": "value"})
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match when header exists")
	}

	ri2 := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{})
	if compiled.Matches(ri2) {
		t.Error("Expected rule to not match when header is absent")
	}
}

func TestHTTPHeaderNameInSingleCaseInsensitive(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyHTTPHeaderName,
		ConditionOperator:       dbgen.RuleConditionOperatorIn,
		ConditionValueStr:       pgtype.Text{String: "x-custom-header", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{"X-Custom-Header": "value"})
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match header name case-insensitively")
	}
}

func TestHTTPHeaderNameInSingleNegated(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:        dbgen.RuleConditionPropertyHTTPHeaderName,
		ConditionOperator:        dbgen.RuleConditionOperatorIn,
		ConditionOperatorNegated: true,
		ConditionValueStr:        pgtype.Text{String: "X-Custom-Header", Valid: true},
		ConditionValueSeparator:  pgtype.Text{String: ",", Valid: true},
		ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:              50,
		Enabled:                  true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{})
	if !compiled.Matches(ri) {
		t.Error("Expected negated rule to match when header is absent")
	}

	ri2 := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{"X-Custom-Header": "value"})
	if compiled.Matches(ri2) {
		t.Error("Expected negated rule to not match when header exists")
	}
}

func TestHTTPHeaderNameEqualsUnsupported(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyHTTPHeaderName,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "X-Custom-Header", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	_, err := testCompiler.CompileRule(context.Background(), rule)
	if err == nil {
		t.Error("Expected error when using equals operator for HTTP header condition")
	}
}

func TestHTTPHeaderNameInMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyHTTPHeaderName,
		ConditionOperator:       dbgen.RuleConditionOperatorIn,
		ConditionValueStr:       pgtype.Text{String: "X-Header-A,X-Header-B", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{"X-Header-A": "val"})
	if !compiled.Matches(ri) {
		t.Error("Expected rule to match when first header in list exists")
	}

	ri2 := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{"X-Header-B": "val"})
	if !compiled.Matches(ri2) {
		t.Error("Expected rule to match when second header in list exists")
	}

	ri3 := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{})
	if compiled.Matches(ri3) {
		t.Error("Expected rule to not match when none of the headers exist")
	}
}

func TestHTTPHeaderNameInNegated(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:        dbgen.RuleConditionPropertyHTTPHeaderName,
		ConditionOperator:        dbgen.RuleConditionOperatorIn,
		ConditionOperatorNegated: true,
		ConditionValueStr:        pgtype.Text{String: "X-Header-A,X-Header-B", Valid: true},
		ConditionValueSeparator:  pgtype.Text{String: ",", Valid: true},
		ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:              50,
		Enabled:                  true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{})
	if !compiled.Matches(ri) {
		t.Error("Expected negated rule to match when no headers in list exist")
	}

	ri2 := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{"X-Header-A": "val"})
	if compiled.Matches(ri2) {
		t.Error("Expected negated rule to not match when a header in list exists")
	}
}

func TestHTTPHeaderNameHasHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	ri := NewRequestInfo(req, "")

	if !ri.HasHeader("X-Forwarded-For") {
		t.Error("Expected HasHeader to return true for existing header")
	}
	if ri.HasHeader("x-forwarded-for") {
		t.Error("Expected HasHeader to return false for non-canonical name (no conversion in HasHeader)")
	}
	if ri.HasHeader("X-Missing-Header") {
		t.Error("Expected HasHeader to return false for missing header")
	}
}

func TestHTTPHeaderNameNonCanonicalInRule(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty:       dbgen.RuleConditionPropertyHTTPHeaderName,
		ConditionOperator:       dbgen.RuleConditionOperatorIn,
		ConditionValueStr:       pgtype.Text{String: "x-header-a,X-HEADER-B", Valid: true},
		ConditionValueSeparator: pgtype.Text{String: ",", Valid: true},
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}

	ri := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{"X-Header-A": "val"})
	if !compiled.Matches(ri) {
		t.Error("Expected rule with lowercase header name in In list to match after canonicalization")
	}

	ri2 := newTestRequestInfoWithHeaders("agent", netip.MustParseAddr("1.2.3.4"), map[string]string{"X-Header-B": "val"})
	if !compiled.Matches(ri2) {
		t.Error("Expected rule with uppercase header name in In list to match after canonicalization")
	}
}

func TestTerminalRulePreventsBlockInIsRequestBlocked(t *testing.T) {
	// A terminal break rule before a block rule should prevent blocking
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyBreak,
			Terminal:          true,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))

	if compiled.IsRequestBlocked(ri) {
		t.Error("Expected terminal break rule to prevent block rule from triggering")
	}
}

func TestBlockRuleStillWorksWithoutTerminalBefore(t *testing.T) {
	// A non-terminal non-block rule before a terminal block rule should NOT prevent blocking
	rules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Position:          1,
			Enabled:           true,
		},
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Terminal:          true,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), rules)
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))

	if !compiled.IsRequestBlocked(ri) {
		t.Error("Expected terminal block rule to still trigger when prior rule is not terminal")
	}
}

func TestOrgBlockNotPreventedByPropertyBreak(t *testing.T) {
	// An org block rule runs before property rules, so a property break rule cannot prevent it
	propertyRules := []*dbgen.DifficultyRule{
		{
			ID:                1,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			PropertyID:        pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyBreak,
			Terminal:          true,
			Position:          1,
			Enabled:           true,
		},
	}
	orgRules := []*dbgen.DifficultyRule{
		{
			ID:                2,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Position:          1,
			Enabled:           true,
		},
	}

	compiledProp := testCompiler.Compile(context.Background(), propertyRules)
	compiledOrg := testCompiler.Compile(context.Background(), orgRules)
	rp := &RulesPair{PropertyRules: compiledProp, OrgRules: compiledOrg}

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	if !rp.IsRequestBlocked(ri) {
		t.Error("Expected org block rule to fire even when property break rule matches")
	}
}
