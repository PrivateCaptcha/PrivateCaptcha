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
)

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

func newStubProperty() *difficulty.StubProperty {
	return difficulty.NewStubProperty(1, true, 1, 1, 50, dbgen.DifficultyGrowthMedium)
}

func TestUserAgentEqualsMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "BadBot/1.0", Valid: true},
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:          dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:             50,
		Enabled:                 true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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

func TestIPAddressMatchesPrefix(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       25,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       100, // doubles the difficulty (+100%)
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       -20, // -20%
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       -99, // -99% should clamp to minimum
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       1000, // +1000% should clamp to maximum
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty() // has level 50

	result := compiled.Apply(prop)
	// 50 * 1100 / 100 = 550, should be clamped to 255
	if result.Level() != 255 {
		t.Errorf("Expected level clamped to 255, got %d", result.Level())
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

	compiled, err := CompileRule(context.Background(), rule)
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
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Position:          1,
			Enabled:           true,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          2,
			Enabled:           true,
		},
	}

	compiled := Compile(context.Background(), propertyRules)
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
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
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
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Position:          1,
			Enabled:           true,
		},
	}

	compiledProp := Compile(context.Background(), propertyRules)
	compiledOrg := Compile(context.Background(), orgRules)
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
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Enabled:           true,
		},
	}

	compiled := Compile(context.Background(), propertyRules)
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
	compiled := Compile(context.Background(), nil)
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

	compiled := Compile(context.Background(), propertyRules)

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
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
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

	compiled := Compile(context.Background(), propertyRules)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	_, err := CompileRule(context.Background(), rule)
	if err != ErrInvalidIPValue {
		t.Errorf("Expected ErrInvalidIPValue, got %v", err)
	}
}

func TestUnknownConditionProperty(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: "unknown_prop",
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "test", Valid: true},
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	_, err := CompileRule(context.Background(), rule)
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

	_, err := CompileRule(context.Background(), rule)
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
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Enabled:           true,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       199,
			Enabled:           false,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       25,
			Enabled:           true,
		},
	}

	compiled := Compile(context.Background(), propertyRules)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
		ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		Enabled:           true,
	}

	compiled, err := CompileRule(context.Background(), rule)
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
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       100,
			Enabled:           true,
		},
	}
	org := Compile(context.Background(), orgRules)
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
			ActionProperty:    dbgen.BackendRuleActionPropertyDifficultyLevelPercent,
			ActionValue:       50,
			Enabled:           true,
		},
	}
	prop := Compile(context.Background(), propRules)
	rp := &RulesPair{PropertyRules: prop}

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	p := newStubProperty()
	result := rp.Apply(ri, p)
	// Base level is 50, rule has +50% so result should be 50 * 1.5 = 75
	if result.Level() != 75 {
		t.Errorf("Expected property rule (+50%% = level 75) when no org rules, got %d", result.Level())
	}
}
