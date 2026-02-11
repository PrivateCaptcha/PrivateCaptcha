package rules

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"

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
		if _, ok := rule.(*blockRequestRule); ok {
			if rule.Matches(ri) {
				return true
			}
		}
	}

	return false
}

// Merge combines property-level and org-level compiled rules.
// Property rules come first (higher priority), then org rules.
func Merge(propertyRules, orgRules *CompiledRules) *CompiledRules {
	if propertyRules == nil && orgRules == nil {
		return nil
	}
	if propertyRules == nil {
		return orgRules
	}
	if orgRules == nil {
		return propertyRules
	}
	combined := make([]Rule, 0, len(propertyRules.rules)+len(orgRules.rules))
	combined = append(combined, propertyRules.rules...)
	combined = append(combined, orgRules.rules...)
	return NewCompiledRules(combined)
}

type overrideProperty struct {
	base   difficulty.Property
	level  *int16
	growth *dbgen.DifficultyGrowth
}

func (op *overrideProperty) ID() int32      { return op.base.ID() }
func (op *overrideProperty) Valid() bool     { return op.base.Valid() }
func (op *overrideProperty) OwnerID() int32  { return op.base.OwnerID() }
func (op *overrideProperty) OrgID() int32    { return op.base.OrgID() }
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

// blockRequestRule blocks matching requests
type blockRequestRule struct {
	matcher matcherFunc
}

func (r *blockRequestRule) Matches(ri *RequestInfo) bool {
	return r.matcher(ri)
}

func (r *blockRequestRule) Apply(p difficulty.Property) difficulty.Property {
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
	separator := rule.ConditionValueSeparator.String

	switch rule.ConditionProperty {
	case dbgen.RuleConditionPropertyUserAgent:
		value := rule.ConditionValueStr.String
		return stringMatcher((*RequestInfo).UserAgent, value, rule.ConditionOperator, separator), nil
	case dbgen.RuleConditionPropertyIPAddress:
		if rule.ConditionOperator == dbgen.RuleConditionOperatorEmpty {
			return ipAddressEmptyMatcher, nil
		}
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
		return stringMatcher((*RequestInfo).CountryCode, value, rule.ConditionOperator, separator), nil
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
		return &blockRequestRule{
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

func ruleNameAttr(rule *dbgen.DifficultyRule) slog.Attr {
	return slog.String("ruleName", rule.Name.String)
}

func Compile(ctx context.Context, dbRules []*dbgen.DifficultyRule) *CompiledRules {
	rules := make([]Rule, 0, len(dbRules))

	for _, r := range dbRules {
		compiled, err := CompileRule(ctx, r)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to compile rule", "ruleID", r.ID, ruleNameAttr(r), common.ErrAttr(err))
			continue
		}
		rules = append(rules, compiled)
	}

	if len(rules) == 0 {
		return nil
	}

	return NewCompiledRules(rules)
}
