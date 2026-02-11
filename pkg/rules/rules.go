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

type Rule interface {
	Matches(ctx context.Context, r *http.Request) bool
	Apply(p difficulty.Property) difficulty.Property
}

type CompiledRules struct {
	rules []Rule
}

func NewCompiledRules(rules []Rule) *CompiledRules {
	return &CompiledRules{rules: rules}
}

func (cr *CompiledRules) Apply(ctx context.Context, r *http.Request, p difficulty.Property) difficulty.Property {
	if cr == nil || len(cr.rules) == 0 {
		return p
	}

	for _, rule := range cr.rules {
		if rule.Matches(ctx, r) {
			return rule.Apply(p)
		}
	}

	return p
}

func (cr *CompiledRules) IsRequestBlocked(ctx context.Context, r *http.Request) bool {
	if cr == nil || len(cr.rules) == 0 {
		return false
	}

	for _, rule := range cr.rules {
		if rule.Matches(ctx, r) {
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
type matcherFunc func(ctx context.Context, r *http.Request) bool

func userAgentEqualsMatcher(value string) matcherFunc {
	return func(_ context.Context, r *http.Request) bool {
		return r.UserAgent() == value
	}
}

func userAgentContainsMatcher(value string) matcherFunc {
	return func(_ context.Context, r *http.Request) bool {
		return strings.Contains(r.UserAgent(), value)
	}
}

func ipAddressMatchesMatcher(prefix netip.Prefix) matcherFunc {
	return func(ctx context.Context, _ *http.Request) bool {
		if ip, ok := ctx.Value(common.RateLimitKeyContextKey).(netip.Addr); ok {
			return prefix.Contains(ip)
		}
		return false
	}
}

func countryCodeMatcher(value string, operator dbgen.RuleConditionOperator) matcherFunc {
	return func(ctx context.Context, r *http.Request) bool {
		headerName, ok := ctx.Value(common.CountryCodeHeaderContextKey).(string)
		if !ok || len(headerName) == 0 {
			return false
		}
		cc := r.Header.Get(headerName)
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

func (r *difficultyLevelRule) Matches(ctx context.Context, req *http.Request) bool {
	return r.matcher(ctx, req)
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

func (r *difficultyGrowthRule) Matches(ctx context.Context, req *http.Request) bool {
	return r.matcher(ctx, req)
}

func (r *difficultyGrowthRule) Apply(p difficulty.Property) difficulty.Property {
	growth := r.growth
	return &overrideProperty{base: p, growth: &growth}
}

// httpRequestRule blocks matching requests
type httpRequestRule struct {
	matcher matcherFunc
}

func (r *httpRequestRule) Matches(ctx context.Context, req *http.Request) bool {
	return r.matcher(ctx, req)
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
