package rules

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/jackc/pgx/v5/pgtype"
)

type testProperty struct {
	id      int32
	valid   bool
	ownerID int32
	orgID   int32
	level   int16
	growth  dbgen.DifficultyGrowth
}

func (p *testProperty) ID() int32                    { return p.id }
func (p *testProperty) Valid() bool                  { return p.valid }
func (p *testProperty) OwnerID() int32               { return p.ownerID }
func (p *testProperty) OrgID() int32                 { return p.orgID }
func (p *testProperty) Level() int16                 { return p.level }
func (p *testProperty) Growth() dbgen.DifficultyGrowth { return p.growth }

var _ difficulty.Property = (*testProperty)(nil)

func newTestRequest(userAgent string, ip netip.Addr) (*http.Request, context.Context) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", userAgent)
	ctx := context.WithValue(req.Context(), common.RateLimitKeyContextKey, ip)
	return req.WithContext(ctx), ctx
}

func newTestRequestWithCountryCode(userAgent string, ip netip.Addr, headerName, countryCode string) (*http.Request, context.Context) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", userAgent)
	if len(countryCode) > 0 {
		req.Header.Set(headerName, countryCode)
	}
	ctx := context.WithValue(req.Context(), common.RateLimitKeyContextKey, ip)
	ctx = context.WithValue(ctx, common.CountryCodeHeaderContextKey, headerName)
	return req.WithContext(ctx), ctx
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
	req, ctx := newTestRequest("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))

	if !compiled.Matches(ctx, req) {
		t.Error("Expected rule to match exact user agent")
	}

	req2, ctx2 := newTestRequest("GoodBot/2.0", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ctx2, req2) {
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
	req, ctx := newTestRequest("Mozilla/5.0 BadBot/1.0", netip.MustParseAddr("1.2.3.4"))

	if !compiled.Matches(ctx, req) {
		t.Error("Expected rule to match containing user agent")
	}

	req2, ctx2 := newTestRequest("Mozilla/5.0", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ctx2, req2) {
		t.Error("Expected rule to not match non-containing user agent")
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

	req, ctx := newTestRequest("test", netip.MustParseAddr("10.1.2.3"))
	if !compiled.Matches(ctx, req) {
		t.Error("Expected rule to match IP in prefix 10.0.0.0/8")
	}

	req2, ctx2 := newTestRequest("test", netip.MustParseAddr("192.168.1.1"))
	if compiled.Matches(ctx2, req2) {
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

	req, ctx := newTestRequest("test", netip.MustParseAddr("192.168.1.100"))
	if !compiled.Matches(ctx, req) {
		t.Error("Expected rule to match exact IP address")
	}

	req2, ctx2 := newTestRequest("test", netip.MustParseAddr("192.168.1.101"))
	if compiled.Matches(ctx2, req2) {
		t.Error("Expected rule to not match different IP")
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
	prop := &testProperty{
		id: 1, valid: true, ownerID: 1, orgID: 1,
		level: 50, growth: dbgen.DifficultyGrowthMedium,
	}

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
	prop := &testProperty{
		id: 1, valid: true, ownerID: 1, orgID: 1,
		level: 50, growth: dbgen.DifficultyGrowthSlow,
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
	prop := &testProperty{
		id: 1, valid: true, ownerID: 1, orgID: 1,
		level: 50, growth: dbgen.DifficultyGrowthMedium,
	}

	req, ctx := newTestRequest("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := compiled.Apply(ctx, req, prop)
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
	prop := &testProperty{
		id: 1, valid: true, ownerID: 1, orgID: 1,
		level: 50, growth: dbgen.DifficultyGrowthMedium,
	}

	req, ctx := newTestRequest("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := compiled.Apply(ctx, req, prop)
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
	prop := &testProperty{
		id: 1, valid: true, ownerID: 1, orgID: 1,
		level: 50, growth: dbgen.DifficultyGrowthMedium,
	}

	req, ctx := newTestRequest("Mozilla/5.0", netip.MustParseAddr("1.2.3.4"))
	result := compiled.Apply(ctx, req, prop)
	if result.Level() != 50 {
		t.Errorf("Expected original level 50 when no match, got %d", result.Level())
	}
}

func TestNilCompiledRulesApply(t *testing.T) {
	prop := &testProperty{
		id: 1, valid: true, ownerID: 1, orgID: 1,
		level: 50, growth: dbgen.DifficultyGrowthMedium,
	}

	req, ctx := newTestRequest("test", netip.MustParseAddr("1.2.3.4"))

	var cr *CompiledRules
	result := cr.Apply(ctx, req, prop)
	if result.Level() != 50 {
		t.Errorf("Expected original property when compiled rules is nil, got level %d", result.Level())
	}

	if cr.IsRequestBlocked(ctx, req) {
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

	req, ctx := newTestRequest("test", netip.MustParseAddr("10.1.2.3"))
	if !compiled.IsRequestBlocked(ctx, req) {
		t.Error("Expected request from 10.1.2.3 to be blocked")
	}

	req2, ctx2 := newTestRequest("test", netip.MustParseAddr("192.168.1.1"))
	if compiled.IsRequestBlocked(ctx2, req2) {
		t.Error("Expected request from 192.168.1.1 to not be blocked")
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

	req, ctx := newTestRequest("test", netip.MustParseAddr("2001:db8::1"))
	if !compiled.Matches(ctx, req) {
		t.Error("Expected rule to match IPv6 address in prefix")
	}

	req2, ctx2 := newTestRequest("test", netip.MustParseAddr("2001:db9::1"))
	if compiled.Matches(ctx2, req2) {
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

	prop := &testProperty{id: 1, valid: true, ownerID: 1, orgID: 1, level: 50, growth: dbgen.DifficultyGrowthMedium}
	req, ctx := newTestRequest("BadBot/1.0", netip.MustParseAddr("1.2.3.4"))
	result := compiled.Apply(ctx, req, prop)
	if result.Level() != 150 {
		t.Errorf("Expected level 150 from valid rule, got %d", result.Level())
	}
}

func TestOverridePropertyPreservesBase(t *testing.T) {
	base := &testProperty{
		id: 42, valid: true, ownerID: 10, orgID: 5,
		level: 80, growth: dbgen.DifficultyGrowthSlow,
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

	req, ctx := newTestRequestWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "US")
	if !compiled.Matches(ctx, req) {
		t.Error("Expected rule to match country code US")
	}

	req2, ctx2 := newTestRequestWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "DE")
	if compiled.Matches(ctx2, req2) {
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

	req, ctx := newTestRequest("test", netip.MustParseAddr("1.2.3.4"))
	if compiled.Matches(ctx, req) {
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

	req, ctx := newTestRequestWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "US")
	if !compiled.Matches(ctx, req) {
		t.Error("Expected rule to match country code containing 'u' (case-insensitive)")
	}

	req2, ctx2 := newTestRequestWithCountryCode("test", netip.MustParseAddr("1.2.3.4"), "X-Country-Code", "DE")
	if compiled.Matches(ctx2, req2) {
		t.Error("Expected rule to not match country code DE for contains 'u'")
	}
}
