package rules

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/medama-io/go-useragent"
)

var (
	ErrUnknownConditionProperty = errors.New("unknown rule condition property")
	ErrUnknownActionProperty    = errors.New("unknown rule action property")
	ErrInvalidIPValue           = errors.New("invalid IP address or prefix value")
)

const (
	defaultSeparator = ","
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

func isBlockedByRules(rules []Rule, ri *RequestInfo) bool {
	for _, rule := range rules {
		if _, ok := rule.(*blockRequestRule); ok {
			if rule.Matches(ri) {
				return true
			}
		}
	}

	return false
}

func (cr *CompiledRules) Apply(ri *RequestInfo, p difficulty.Property) (difficulty.Property, bool) {
	if (cr == nil) || (len(cr.rules) == 0) || (ri == nil) {
		return p, false
	}

	for _, rule := range cr.rules {
		if rule.Matches(ri) {
			return rule.Apply(p), true
		}
	}

	return p, false
}

func (cr *CompiledRules) IsRequestBlocked(ri *RequestInfo) bool {
	if (cr == nil) || (len(cr.rules) == 0) || (ri == nil) {
		return false
	}
	return isBlockedByRules(cr.rules, ri)
}

// RulesPair combines property-level and org-level compiled rules
// without additional allocations. Property rules have higher priority.
type RulesPair struct {
	PropertyRules *CompiledRules
	OrgRules      *CompiledRules
}

func (rp *RulesPair) Apply(ri *RequestInfo, p difficulty.Property) difficulty.Property {
	if (rp == nil) || (ri == nil) {
		return p
	}

	if result, found := rp.PropertyRules.Apply(ri, p); found {
		return result
	}

	if result, found := rp.OrgRules.Apply(ri, p); found {
		return result
	}

	return p
}

func (rp *RulesPair) IsRequestBlocked(ri *RequestInfo) bool {
	if (rp == nil) || (ri == nil) {
		return false
	}

	if rp.PropertyRules != nil && isBlockedByRules(rp.PropertyRules.rules, ri) {
		return true
	}

	if rp.OrgRules != nil && isBlockedByRules(rp.OrgRules.rules, ri) {
		return true
	}

	return false
}

type overrideProperty struct {
	base   difficulty.Property
	level  *int16
	growth *dbgen.DifficultyGrowth
	ruleID int32
}

func (op *overrideProperty) ID() int32      { return op.base.ID() }
func (op *overrideProperty) Valid() bool    { return op.base.Valid() }
func (op *overrideProperty) OwnerID() int32 { return op.base.OwnerID() }
func (op *overrideProperty) OrgID() int32   { return op.base.OrgID() }
func (op *overrideProperty) RuleID() int32  { return op.ruleID }
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

// ruleBase contains common fields for all rule types
type ruleBase struct {
	ruleID int32
}

// difficultyLevelRule adjusts the difficulty level by a percentage for a property
type difficultyLevelRule struct {
	ruleBase
	matcher     matcher
	percentDiff int16 // percentage difference (e.g., +20 means +20%, -20 means -20%)
}

func (r *difficultyLevelRule) Matches(ri *RequestInfo) bool {
	return r.matcher.matches(ri)
}

func (r *difficultyLevelRule) Apply(p difficulty.Property) difficulty.Property {
	baseLevel := p.Level()
	// Calculate adjusted level: baseLevel * (100 + percentDiff) / 100
	// Add 50 to numerator for rounding to nearest integer
	adjustedLevel := int16((int32(baseLevel)*(100+int32(r.percentDiff)) + 50) / 100)
	// Clamp to valid range [1, 255]
	if adjustedLevel < int16(common.MinDifficultyLevel) {
		adjustedLevel = int16(common.MinDifficultyLevel)
	} else if adjustedLevel > int16(common.MaxDifficultyLevel) {
		adjustedLevel = int16(common.MaxDifficultyLevel)
	}
	return &overrideProperty{base: p, level: &adjustedLevel, ruleID: r.ruleID}
}

// difficultyGrowthRule overrides the difficulty growth for a property
type difficultyGrowthRule struct {
	ruleBase
	matcher matcher
	growth  dbgen.DifficultyGrowth
}

func (r *difficultyGrowthRule) Matches(ri *RequestInfo) bool {
	return r.matcher.matches(ri)
}

func (r *difficultyGrowthRule) Apply(p difficulty.Property) difficulty.Property {
	growth := r.growth
	return &overrideProperty{base: p, growth: &growth, ruleID: r.ruleID}
}

// blockRequestRule blocks matching requests
type blockRequestRule struct {
	ruleBase
	matcher matcher
}

func (r *blockRequestRule) Matches(ri *RequestInfo) bool {
	return r.matcher.matches(ri)
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

// Compiler is the interface for compiling database rules into executable rule objects.
type Compiler interface {
	Compile(ctx context.Context, dbRules []*dbgen.DifficultyRule) *CompiledRules
}

// RulesCompiler compiles database rules into executable rule objects.
// It owns a reference to the user agent parser for bot detection.
type RulesCompiler struct {
	uaParser *useragent.Parser
}

// NewRulesCompiler creates a new RulesCompiler with the provided user agent parser.
func NewRulesCompiler(uaParser *useragent.Parser) *RulesCompiler {
	return &RulesCompiler{uaParser: uaParser}
}

var _ Compiler = (*RulesCompiler)(nil)

func (rc *RulesCompiler) buildMatcher(rule *dbgen.DifficultyRule) (matcher, error) {
	switch rule.ConditionProperty {
	case dbgen.RuleConditionPropertyUserAgent:
		if rule.ConditionOperator == dbgen.RuleConditionOperatorBot {
			return &botMatcher{
				uaParser:                 rc.uaParser,
				conditionOperatorNegated: rule.ConditionOperatorNegated,
			}, nil
		}
		fallthrough
	case dbgen.RuleConditionPropertyCountryCode, dbgen.RuleConditionPropertyDomain:
		value := rule.ConditionValueStr.String
		sm := &stringMatcher{
			conditionProperty:        rule.ConditionProperty,
			conditionOperator:        rule.ConditionOperator,
			conditionValueStr:        value,
			conditionOperatorNegated: rule.ConditionOperatorNegated,
		}

		// Pre-process values for optimized matching
		if rule.ConditionOperator == dbgen.RuleConditionOperatorIn {
			sep := defaultSeparator
			if rule.ConditionValueSeparator.Valid && len(rule.ConditionValueSeparator.String) > 0 {
				sep = rule.ConditionValueSeparator.String
			}
			items := strings.Split(value, sep)
			for i, item := range items {
				items[i] = strings.TrimSpace(item)
			}
			sm.conditionValueItems = items
		}

		return sm, nil

	case dbgen.RuleConditionPropertyIPAddress:
		im := &ipMatcher{
			conditionOperator:        rule.ConditionOperator,
			conditionOperatorNegated: rule.ConditionOperatorNegated,
		}

		if rule.ConditionOperator != dbgen.RuleConditionOperatorEmpty {
			value := rule.ConditionValueStr.String
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				addr, addrErr := netip.ParseAddr(value)
				if addrErr != nil {
					return nil, ErrInvalidIPValue
				}
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
			im.conditionValueIPPrefix = prefix
		}

		return im, nil

	default:
		return nil, ErrUnknownConditionProperty
	}
}

func (rc *RulesCompiler) CompileRule(ctx context.Context, rule *dbgen.DifficultyRule) (Rule, error) {
	matcher, err := rc.buildMatcher(rule)
	if err != nil {
		return nil, err
	}

	switch rule.ActionProperty {
	case dbgen.RuleActionPropertyDifficultyLevelPercent:
		percentDiff := max(-100, min(100, rule.ActionValue))
		return &difficultyLevelRule{
			ruleBase:    ruleBase{ruleID: rule.ID},
			matcher:     matcher,
			percentDiff: int16(percentDiff),
		}, nil
	case dbgen.RuleActionPropertyHTTPRequest:
		return &blockRequestRule{
			ruleBase: ruleBase{ruleID: rule.ID},
			matcher:  matcher,
		}, nil
	case dbgen.RuleActionPropertyDifficultyGrowth:
		return &difficultyGrowthRule{
			ruleBase: ruleBase{ruleID: rule.ID},
			matcher:  matcher,
			growth:   growthFromInt(rule.ActionValue),
		}, nil
	default:
		return nil, ErrUnknownActionProperty
	}
}

func (rc *RulesCompiler) Compile(ctx context.Context, dbRules []*dbgen.DifficultyRule) *CompiledRules {
	rules := make([]Rule, 0, len(dbRules))

	for _, r := range dbRules {
		if !r.Enabled {
			continue
		}

		compiled, err := rc.CompileRule(ctx, r)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to compile rule", "ruleID", r.ID, "ruleName", r.Name, common.ErrAttr(err))
			continue
		}

		rules = append(rules, compiled)
	}

	if len(rules) == 0 {
		return nil
	}

	return NewCompiledRules(rules)
}
