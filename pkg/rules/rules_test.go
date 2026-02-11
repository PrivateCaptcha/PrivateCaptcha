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
	return &difficulty.StubProperty{
		PropertyID: 1, IsValid: true, PropertyOwnerID: 1, PropertyOrgID: 1,
		PropertyLevel: 50, PropertyGrowth: dbgen.DifficultyGrowthMedium,
	}
}

func TestUserAgentEqualsMatch(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorEquals,
		ConditionValueStr: pgtype.Text{String: "BadBot/1.0", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:          dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:             200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       150,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
	}

	compiled, err := CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := newStubProperty()

	result := compiled.Apply(prop)
	if result.Level() != 200 {
		t.Errorf("Expected level 200, got %d", result.Level())
	}
	if result.Growth() != dbgen.DifficultyGrowthMedium {
		t.Errorf("Expected growth to remain medium, got %s", result.Growth())
	}
}

func TestDifficultyGrowthApply(t *testing.T) {
	rule := &dbgen.DifficultyRule{
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
		ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
		ActionValue:       3, // fast
	}

	compiled, err := CompileRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	prop := &difficulty.StubProperty{
		PropertyID: 1, IsValid: true, PropertyOwnerID: 1, PropertyOrgID: 1,
		PropertyLevel: 50, PropertyGrowth: dbgen.DifficultyGrowthSlow,
	}

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
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       200,
			Position:          1,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       100,
			Position:          2,
		},
	}

	compiled := Compile(context.Background(), propertyRules, nil)
	prop := newStubProperty()

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := compiled.Apply(ri, prop)
	if result.Level() != 200 {
		t.Errorf("Expected first matching rule (level 200), got %d", result.Level())
	}
}

func TestCompiledRulesPropertyBeforeOrg(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			PropertyID:        pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       180,
			Position:          1,
		},
	}
	orgRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			OrgID:             pgtype.Int4{Int32: 1, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       100,
			Position:          1,
		},
	}

	compiled := Compile(context.Background(), propertyRules, orgRules)
	prop := newStubProperty()

	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := compiled.Apply(ri, prop)
	if result.Level() != 180 {
		t.Errorf("Expected property-level rule (180) to take precedence, got %d", result.Level())
	}
}

func TestCompiledRulesNoMatch(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorEquals,
			ConditionValueStr: pgtype.Text{String: "SpecificBot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       200,
		},
	}

	compiled := Compile(context.Background(), propertyRules, nil)
	prop := newStubProperty()

	ri := newTestRequestInfo("Mozilla/5.0", netip.MustParseAddr("1.2.3.4"))
	result := compiled.Apply(ri, prop)
	if result.Level() != 50 {
		t.Errorf("Expected original level 50 when no match, got %d", result.Level())
	}
}

func TestNilCompiledRulesApply(t *testing.T) {
	prop := newStubProperty()

	ri := newTestRequestInfo("test", netip.MustParseAddr("1.2.3.4"))

	var cr *CompiledRules
	result := cr.Apply(ri, prop)
	if result.Level() != 50 {
		t.Errorf("Expected original property when compiled rules is nil, got level %d", result.Level())
	}

	if cr.IsRequestBlocked(ri) {
		t.Error("Expected nil compiled rules to not block request")
	}
}

func TestEmptyCompileReturnsNil(t *testing.T) {
	compiled := Compile(context.Background(), nil, nil)
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
		},
	}

	compiled := Compile(context.Background(), propertyRules, nil)

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
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       200,
			Position:          1,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator: dbgen.RuleConditionOperatorMatches,
			ConditionValueStr: pgtype.Text{String: "10.0.0.0/8", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       1,
			Position:          2,
		},
	}

	compiled := Compile(context.Background(), propertyRules, nil)
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionValue:       200,
	}

	_, err := CompileRule(context.Background(), rule)
	if err != ErrUnknownActionProperty {
		t.Errorf("Expected ErrUnknownActionProperty, got %v", err)
	}
}

func TestCompileSkipsInvalidRules(t *testing.T) {
	propertyRules := []*dbgen.DifficultyRule{
		{
			ConditionProperty: "unknown_prop",
			ConditionOperator: dbgen.RuleConditionOperatorEquals,
			ConditionValueStr: pgtype.Text{String: "test", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       200,
		},
		{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: pgtype.Text{String: "Bot", Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       150,
		},
	}

	compiled := Compile(context.Background(), propertyRules, nil)
	if compiled == nil {
		t.Fatal("Expected non-nil CompiledRules (one valid rule)")
	}

	prop := newStubProperty()
	ri := newTestRequestInfo("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := compiled.Apply(ri, prop)
	if result.Level() != 150 {
		t.Errorf("Expected level 150 from valid rule, got %d", result.Level())
	}
}

func TestOverridePropertyPreservesBase(t *testing.T) {
	base := &difficulty.StubProperty{
		PropertyID: 42, IsValid: true, PropertyOwnerID: 10, PropertyOrgID: 5,
		PropertyLevel: 80, PropertyGrowth: dbgen.DifficultyGrowthSlow,
	}
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
		ActionValue:       200,
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
