package rules

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
)

var (
	ErrUnknownConditionProperty = errors.New("unknown rule condition property")
	ErrUnknownActionProperty    = errors.New("unknown rule action property")
	ErrInvalidIPValue           = errors.New("invalid IP address or prefix value")
)

// RequestInfo wraps http.Request and lazy-caches request attributes for rule matching
type RequestInfo struct {
	r                 *http.Request
	countryCodeHeader string

	userAgent   *string
	ipAddr      *netip.Addr
	countryCode *string
}

func NewRequestInfo(r *http.Request, countryCodeHeader string) *RequestInfo {
	return &RequestInfo{
		r:                 r,
		countryCodeHeader: countryCodeHeader,
	}
}

func (ri *RequestInfo) UserAgent() string {
	if ri.userAgent == nil {
		ua := ri.r.UserAgent()
		ri.userAgent = &ua
	}
	return *ri.userAgent
}

func (ri *RequestInfo) IPAddr() (netip.Addr, bool) {
	if ri.ipAddr == nil {
		if ip, ok := ri.r.Context().Value(common.RateLimitKeyContextKey).(netip.Addr); ok {
			ri.ipAddr = &ip
		} else {
			return netip.Addr{}, false
		}
	}
	return *ri.ipAddr, true
}

func (ri *RequestInfo) CountryCode() string {
	if ri.countryCode == nil {
		var cc string
		if len(ri.countryCodeHeader) > 0 {
			cc = ri.r.Header.Get(ri.countryCodeHeader)
		}
		ri.countryCode = &cc
	}
	return *ri.countryCode
}

type Rule interface {
	Matches(ri *RequestInfo) bool
	Apply(p difficulty.Property) difficulty.Property
}

type CompiledRules struct {
	rules []Rule
}

func NewCompiledRules(rules []Rule) *CompiledRules {
	return &CompiledRules{rules: rules}
}

func (cr *CompiledRules) Apply(ri *RequestInfo, p difficulty.Property) difficulty.Property {
	if cr == nil || len(cr.rules) == 0 {
		return p
	}

	for _, rule := range cr.rules {
		if rule.Matches(ri) {
			return rule.Apply(p)
		}
	}

	return p
}

func (cr *CompiledRules) IsRequestBlocked(ri *RequestInfo) bool {
	if cr == nil || len(cr.rules) == 0 {
		return false
	}

	for _, rule := range cr.rules {
		if rule.Matches(ri) {
			if _, ok := rule.(*httpRequestRule); ok {
				return true
			}
		}
	}

	return false
}

type overrideProperty struct {
	base   difficulty.Property
	level  *int16
	growth *dbgen.DifficultyGrowth
}

func (op *overrideProperty) ID() int32                    { return op.base.ID() }
func (op *overrideProperty) Valid() bool                  { return op.base.Valid() }
func (op *overrideProperty) OwnerID() int32               { return op.base.OwnerID() }
func (op *overrideProperty) OrgID() int32                 { return op.base.OrgID() }
func (op *overrideProperty) Level() int16 {
	if op.level != nil {
		return *op.level
	}
	return op.base.Level()
}
func (op *overrideProperty) Growth() dbgen.DifficultyGrowth {
	if op.growth != nil {
		return *op.growth
	}
	return op.base.Growth()
}

// matcherFunc returns true when the request matches the rule condition
type matcherFunc func(ri *RequestInfo) bool

func userAgentEqualsMatcher(value string) matcherFunc {
	return func(ri *RequestInfo) bool {
		return ri.UserAgent() == value
	}
}

func userAgentContainsMatcher(value string) matcherFunc {
	return func(ri *RequestInfo) bool {
		return strings.Contains(ri.UserAgent(), value)
	}
}

func ipAddressMatchesMatcher(prefix netip.Prefix) matcherFunc {
	return func(ri *RequestInfo) bool {
		if ip, ok := ri.IPAddr(); ok {
			return prefix.Contains(ip)
		}
		return false
	}
}

func countryCodeMatcher(value string, operator dbgen.RuleConditionOperator) matcherFunc {
	return func(ri *RequestInfo) bool {
		cc := ri.CountryCode()
		if len(cc) == 0 {
			return false
		}
		switch operator {
		case dbgen.RuleConditionOperatorEquals:
			return strings.EqualFold(cc, value)
		case dbgen.RuleConditionOperatorContains:
			return strings.Contains(strings.ToLower(cc), strings.ToLower(value))
		default:
			return strings.EqualFold(cc, value)
		}
	}
}

// difficultyLevelRule overrides the difficulty level for a property
type difficultyLevelRule struct {
	matcher matcherFunc
	level   int16
}

func (r *difficultyLevelRule) Matches(ri *RequestInfo) bool {
	return r.matcher(ri)
}

func (r *difficultyLevelRule) Apply(p difficulty.Property) difficulty.Property {
	level := r.level
	return &overrideProperty{base: p, level: &level}
}

// difficultyGrowthRule overrides the difficulty growth for a property
type difficultyGrowthRule struct {
	matcher matcherFunc
	growth  dbgen.DifficultyGrowth
}

func (r *difficultyGrowthRule) Matches(ri *RequestInfo) bool {
	return r.matcher(ri)
}

func (r *difficultyGrowthRule) Apply(p difficulty.Property) difficulty.Property {
	growth := r.growth
	return &overrideProperty{base: p, growth: &growth}
}

// httpRequestRule blocks matching requests
type httpRequestRule struct {
	matcher matcherFunc
}

func (r *httpRequestRule) Matches(ri *RequestInfo) bool {
	return r.matcher(ri)
}

func (r *httpRequestRule) Apply(p difficulty.Property) difficulty.Property {
	return p
}

func growthFromInt(value int32) dbgen.DifficultyGrowth {
	switch value {
	case 0:
		return dbgen.DifficultyGrowthConstant
	case 1:
		return dbgen.DifficultyGrowthSlow
	case 2:
		return dbgen.DifficultyGrowthMedium
	case 3:
		return dbgen.DifficultyGrowthFast
	default:
		return dbgen.DifficultyGrowthMedium
	}
}

func buildMatcher(rule *dbgen.DifficultyRule) (matcherFunc, error) {
	switch rule.ConditionProperty {
	case dbgen.RuleConditionPropertyUserAgent:
		value := rule.ConditionValueStr.String
		switch rule.ConditionOperator {
		case dbgen.RuleConditionOperatorEquals:
			return userAgentEqualsMatcher(value), nil
		case dbgen.RuleConditionOperatorContains:
			return userAgentContainsMatcher(value), nil
		default:
			return userAgentContainsMatcher(value), nil
		}
	case dbgen.RuleConditionPropertyIPAddress:
		value := rule.ConditionValueStr.String
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			addr, addrErr := netip.ParseAddr(value)
			if addrErr != nil {
				return nil, ErrInvalidIPValue
			}
			bits := 32
			if addr.Is6() {
				bits = 128
			}
			prefix = netip.PrefixFrom(addr, bits)
		}
		return ipAddressMatchesMatcher(prefix), nil
	case dbgen.RuleConditionPropertyCountryCode:
		value := rule.ConditionValueStr.String
		return countryCodeMatcher(value, rule.ConditionOperator), nil
	default:
		return nil, ErrUnknownConditionProperty
	}
}

func CompileRule(ctx context.Context, rule *dbgen.DifficultyRule) (Rule, error) {
	matcher, err := buildMatcher(rule)
	if err != nil {
		return nil, err
	}

	switch rule.ActionProperty {
	case dbgen.RuleActionPropertyDifficultyLevel:
		return &difficultyLevelRule{
			matcher: matcher,
			level:   int16(rule.ActionValue),
		}, nil
	case dbgen.RuleActionPropertyHTTPRequest:
		return &httpRequestRule{
			matcher: matcher,
		}, nil
	case dbgen.RuleActionPropertyDifficultyGrowth:
		return &difficultyGrowthRule{
			matcher: matcher,
			growth:  growthFromInt(rule.ActionValue),
		}, nil
	default:
		return nil, ErrUnknownActionProperty
	}
}

func Compile(ctx context.Context, propertyRules, orgRules []*dbgen.DifficultyRule) *CompiledRules {
	rules := make([]Rule, 0, len(propertyRules)+len(orgRules))

	for _, r := range propertyRules {
		compiled, err := CompileRule(ctx, r)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to compile property rule", "ruleID", r.ID, common.ErrAttr(err))
			continue
		}
		rules = append(rules, compiled)
	}

	for _, r := range orgRules {
		compiled, err := CompileRule(ctx, r)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to compile org rule", "ruleID", r.ID, common.ErrAttr(err))
			continue
		}
		rules = append(rules, compiled)
	}

	if len(rules) == 0 {
		return nil
	}

	return NewCompiledRules(rules)
}
