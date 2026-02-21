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
	defaultSeparator   = ","
	MaxIPAddressValues = 10
)

type Rule interface {
	Matches(ri *RequestInfo) bool
	Apply(p difficulty.Property) difficulty.Property
	isTerminal() bool
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

	var result *overrideProperty
	anyMatched := false

	for _, rule := range cr.rules {
		if !rule.Matches(ri) {
			continue
		}

		applied := rule.Apply(p)
		op, isOverride := applied.(*overrideProperty)
		if !isOverride {
			// blockRequestRule returns the original property unchanged
			anyMatched = true
			if rule.isTerminal() {
				if result != nil {
					return result, true
				}
				return p, true
			}
			continue
		}

		if result == nil {
			result = &overrideProperty{base: p, ruleID: op.ruleID}
		}

		// Accumulate maximum level
		if op.level != nil {
			if result.level == nil || *op.level > *result.level {
				level := *op.level
				result.level = &level
				result.ruleID = op.ruleID
			}
		}

		// Accumulate maximum growth
		if op.growth != nil {
			if result.growth == nil || growthOrder(*op.growth) > growthOrder(*result.growth) {
				growth := *op.growth
				result.growth = &growth
				result.ruleID = op.ruleID
			}
		}

		anyMatched = true

		if rule.isTerminal() {
			return result, true
		}
	}

	if result != nil {
		return result, false
	}

	if anyMatched {
		return p, false
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

	current := p
	propResult, terminal := rp.PropertyRules.Apply(ri, current)
	if propResult != current {
		current = propResult
	}

	if terminal {
		return current
	}

	orgResult, _ := rp.OrgRules.Apply(ri, current)
	if orgResult != current {
		current = orgResult
	}

	return current
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
	ruleID   int32
	terminal bool
}

func (rb *ruleBase) isTerminal() bool { return rb.terminal }

// difficultyLevelRule adjusts the difficulty level by a percentage for a property
type difficultyLevelRule struct {
	ruleBase
	matcher     Matcher
	percentDiff int16 // percentage difference (e.g., +20 means +20%, -20 means -20%)
}

func (r *difficultyLevelRule) Matches(ri *RequestInfo) bool {
	return r.matcher.Matches(ri)
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
	matcher Matcher
	growth  dbgen.DifficultyGrowth
}

func (r *difficultyGrowthRule) Matches(ri *RequestInfo) bool {
	return r.matcher.Matches(ri)
}

func (r *difficultyGrowthRule) Apply(p difficulty.Property) difficulty.Property {
	growth := r.growth
	return &overrideProperty{base: p, growth: &growth, ruleID: r.ruleID}
}

// blockRequestRule blocks matching requests
type blockRequestRule struct {
	ruleBase
	matcher Matcher
}

func (r *blockRequestRule) Matches(ri *RequestInfo) bool {
	return r.matcher.Matches(ri)
}

func (r *blockRequestRule) Apply(p difficulty.Property) difficulty.Property {
	return p
}

// breakRule stops processing following rules
type breakRule struct {
	ruleBase
	matcher Matcher
}

func (r *breakRule) Matches(ri *RequestInfo) bool {
	return r.matcher.Matches(ri)
}

func (r *breakRule) Apply(p difficulty.Property) difficulty.Property {
	return &overrideProperty{base: p, ruleID: r.ruleID}
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

func growthOrder(g dbgen.DifficultyGrowth) int {
	switch g {
	case dbgen.DifficultyGrowthConstant:
		return 0
	case dbgen.DifficultyGrowthSlow:
		return 1
	case dbgen.DifficultyGrowthMedium:
		return 2
	case dbgen.DifficultyGrowthFast:
		return 3
	default:
		return 2
	}
}

// Compiler is the interface for compiling database rules into executable rule objects.
type Compiler interface {
	Compile(ctx context.Context, dbRules []*dbgen.DifficultyRule) *CompiledRules
}

// MatcherFactory creates a Matcher from a database rule.
type MatcherFactory func(rule *dbgen.DifficultyRule) (Matcher, error)

// BuildStringMatcher creates a StringMatcher from a database rule.
func BuildStringMatcher(rule *dbgen.DifficultyRule) (Matcher, error) {
	value := rule.ConditionValueStr.String
	sm := &StringMatcher{
		ConditionProperty:        rule.ConditionProperty,
		ConditionOperator:        rule.ConditionOperator,
		ConditionValueStr:        value,
		ConditionOperatorNegated: rule.ConditionOperatorNegated,
	}

	if rule.ConditionOperator == dbgen.RuleConditionOperatorIn {
		sep := defaultSeparator
		if rule.ConditionValueSeparator.Valid && len(rule.ConditionValueSeparator.String) > 0 {
			sep = rule.ConditionValueSeparator.String
		}
		items := strings.Split(value, sep)
		sm.ConditionValueItems = make([]string, 0, len(items))
		for _, item := range items {
			var item = strings.TrimSpace(item)
			if len(item) == 0 {
				continue
			}
			sm.ConditionValueItems = append(sm.ConditionValueItems, item)
		}
	}

	return sm, nil
}

// BuildIPMatcher creates an IPMatcher from a database rule.
func BuildIPMatcher(rule *dbgen.DifficultyRule) (Matcher, error) {
	im := &IPMatcher{
		ConditionOperator:        rule.ConditionOperator,
		ConditionOperatorNegated: rule.ConditionOperatorNegated,
	}

	if rule.ConditionOperator != dbgen.RuleConditionOperatorEmpty {
		sep := defaultSeparator
		if rule.ConditionValueSeparator.Valid && len(rule.ConditionValueSeparator.String) > 0 {
			sep = rule.ConditionValueSeparator.String
		}
		value := rule.ConditionValueStr.String
		items := strings.Split(value, sep)
		for _, item := range items {
			item = strings.TrimSpace(item)
			if len(item) == 0 {
				continue
			}
			prefix, err := netip.ParsePrefix(item)
			if err != nil {
				addr, addrErr := netip.ParseAddr(item)
				if addrErr != nil {
					return nil, ErrInvalidIPValue
				}
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
			im.ConditionValueIPPrefixes = append(im.ConditionValueIPPrefixes, prefix)
		}
		if len(im.ConditionValueIPPrefixes) == 0 {
			return nil, ErrInvalidIPValue
		}
	}

	return im, nil
}

// RulesCompiler compiles database rules into executable rule objects.
type RulesCompiler struct {
	uaParser  *useragent.Parser
	factories map[string]MatcherFactory
}

// NewRulesCompiler creates a new RulesCompiler with the provided user agent parser.
func NewRulesCompiler(uaParser *useragent.Parser) *RulesCompiler {
	rc := &RulesCompiler{
		uaParser:  uaParser,
		factories: make(map[string]MatcherFactory),
	}
	rc.registerDefaultFactories()
	return rc
}

func (rc *RulesCompiler) registerDefaultFactories() {
	rc.factories[string(dbgen.RuleConditionPropertyUserAgent)] = rc.buildUserAgentMatcher
	rc.factories[string(dbgen.RuleConditionPropertyCountryCode)] = BuildStringMatcher
	rc.factories[string(dbgen.RuleConditionPropertyDomain)] = BuildStringMatcher
	rc.factories[string(dbgen.RuleConditionPropertyIPAddress)] = BuildIPMatcher
}

// RegisterMatcherFactory registers a MatcherFactory for the given condition property,
// replacing any existing factory for that property.
func (rc *RulesCompiler) RegisterMatcherFactory(property string, factory MatcherFactory) {
	rc.factories[property] = factory
}

func (rc *RulesCompiler) buildUserAgentMatcher(rule *dbgen.DifficultyRule) (Matcher, error) {
	if rule.ConditionOperator == dbgen.RuleConditionOperatorBot {
		return &BotMatcher{
			UAParser:                 rc.uaParser,
			ConditionOperatorNegated: rule.ConditionOperatorNegated,
		}, nil
	}
	return BuildStringMatcher(rule)
}

var _ Compiler = (*RulesCompiler)(nil)

func (rc *RulesCompiler) buildMatcher(rule *dbgen.DifficultyRule) (Matcher, error) {
	factory, ok := rc.factories[string(rule.ConditionProperty)]
	if !ok {
		return nil, ErrUnknownConditionProperty
	}
	return factory(rule)
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
			ruleBase:    ruleBase{ruleID: rule.ID, terminal: rule.Terminal},
			matcher:     matcher,
			percentDiff: int16(percentDiff),
		}, nil
	case dbgen.RuleActionPropertyHTTPRequest:
		return &blockRequestRule{
			ruleBase: ruleBase{ruleID: rule.ID, terminal: rule.Terminal},
			matcher:  matcher,
		}, nil
	case dbgen.RuleActionPropertyDifficultyGrowth:
		return &difficultyGrowthRule{
			ruleBase: ruleBase{ruleID: rule.ID, terminal: rule.Terminal},
			matcher:  matcher,
			growth:   growthFromInt(rule.ActionValue),
		}, nil
	case dbgen.RuleActionPropertyBreak:
		return &breakRule{
			ruleBase: ruleBase{ruleID: rule.ID, terminal: rule.Terminal},
			matcher:  matcher,
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
