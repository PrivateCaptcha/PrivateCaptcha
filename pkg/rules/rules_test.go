package rules

import (
	"context"
	"net/http/httptest"
	"net/netip"
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

func newStubProperty() *difficulty.StubProperty {
	return difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthMedium)
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
		ActionValue:       100, // doubles the difficulty (+100%)
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty() // has level 50

	result := compiled.Apply(prop)
	// Base level is 50, +100% doubles to 100
	if result.Level() != 100 {
		t.Errorf("Expected level 100 (50 + 100%%), got %d", result.Level())
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

	result := compiled.Apply(prop)
	// Base level is 50, -20% gives 40
	if result.Level() != 40 {
		t.Errorf("Expected level 40 (50 - 20%%), got %d", result.Level())
	}
}

func TestDifficultyLevelClampingLow(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Test", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       -99, // -99% should clamp to minimum
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty() // has level 50

	result := compiled.Apply(prop)
	// 50 * 1 / 100 with rounding = 1 (rounded up from 0.5), then clamped to minimum 1
	if result.Level() != 1 {
		t.Errorf("Expected level clamped to 1, got %d", result.Level())
	}
}

func TestDifficultyLevelClampingHigh(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Test", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       1000, // +1000% should clamp to maximum
		Enabled:           true,
	}

	compiled, err := testCompiler.CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty() // has level 50

	result := compiled.Apply(prop)
	// 50 * 1100 / 100 = 550, should be clamped to 255
	if result.Level() != 100 {
		t.Errorf("Expected level clamped to 100, got %d", result.Level())
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

	result := compiled.Apply(prop)
	if result.Growth() != dbgen.DifficultyGrowthFast {
		t.Errorf("Expected growth to be fast, got %s", result.Growth())
	}
	if result.Level() != 50 {
		t.Errorf("Expected level to remain 50, got %d", result.Level())
	}
}

func TestCompiledRulesApplyFirstMatch(t *testing.T) {
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
	result, found := compiled.Apply(ri, prop)
	// Base level is 50, first rule has +50% so result should be 50 * 1.5 = 75
	if !found || result.Level() != 75 {
		t.Errorf("Expected first matching rule (+50%% = level 75), got %d", result.Level())
	}
}

func TestRulesPairPropertyBeforeOrg(t *testing.T) {
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
	// Base level is 50, property rule has +30% so result should be 50 * 1.3 = 65
	if result.Level() != 65 {
		t.Errorf("Expected property-level rule (+30%% = level 65) to take precedence, got %d", result.Level())
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
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := testCompiler.Compile(context.Background(), propertyRules)
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("10.1.2.3"))
	if !compiled.IsRequestBlocked(ri) {
		t.Error("Expected block rule to still be checked even when non-block rule also matches")
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
	result, found := compiled.Apply(ri, prop)
	// Base level is 50, valid rule has +25% so result should be 50 * 1.25 = 62.5 rounded to 63
	if !found || result.Level() != 63 {
		t.Errorf("Expected level 63 (+25%% with rounding) from valid rule, got %d", result.Level())
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
	if result.Level() != 100 {
		t.Errorf("Expected org rule (100) when no property rules, got %d", result.Level())
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
	// Base level is 50, rule has +50% so result should be 50 * 1.5 = 75
	if result.Level() != 75 {
		t.Errorf("Expected property rule (+50%% = level 75) when no org rules, got %d", result.Level())
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
