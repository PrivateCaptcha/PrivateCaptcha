package rules

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"sort"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/medama-io/go-useragent"
)

var (
	ErrUnknownConditionProperty     = errors.New("unknown rule condition property")
	ErrUnknownActionProperty        = errors.New("unknown rule action property")
	ErrInvalidIPValue               = errors.New("invalid IP address or prefix value")
	ErrUnsupportedConditionOperator = errors.New("unsupported condition operator for rule")
)

const (
	defaultSeparator   = ","
	MaxIPAddressValues = 10
)

type rule interface {
	Matches(ri *RequestInfo) bool
	Apply(op *overrideProperty) bool
	IsTerminal() bool
}

type CompiledRules struct {
	rules            []rule
	hasBlockRules    bool
	hasTerminalRules bool
}

func NewCompiledRules(rules []rule) *CompiledRules {
	cr := &CompiledRules{rules: rules}
	for _, r := range rules {
		if _, ok := r.(*blockRequestRule); ok {
			cr.hasBlockRules = true
		}
		if r.IsTerminal() {
			cr.hasTerminalRules = true
		}
	}
	return cr
}

func isBlockedByRules(rules []rule, ri *RequestInfo) (blocked bool, terminal bool) {
	for _, r := range rules {
		if !r.Matches(ri) {
			continue
		}
		if _, ok := r.(*blockRequestRule); ok {
			return true, true
		}
		if r.IsTerminal() {
			return false, true
		}
	}

	return false, false
}

func (cr *CompiledRules) Apply(ri *RequestInfo, p difficulty.Property) (difficulty.Property, bool) {
	if (cr == nil) || (len(cr.rules) == 0) || (ri == nil) {
		return p, false
	}

	op := &overrideProperty{base: p}
	anyMatched := false

	for _, rule := range cr.rules {
		if !rule.Matches(ri) {
			continue
		}

		anyMatched = true

		if rule.Apply(op) {
			return op, true
		}
	}

	if anyMatched {
		return op, false
	}

	return p, false
}

func (cr *CompiledRules) IsRequestBlocked(ri *RequestInfo) bool {
	blocked, _ := cr.checkRequestBlocked(ri)
	return blocked
}

func (cr *CompiledRules) checkRequestBlocked(ri *RequestInfo) (blocked bool, terminal bool) {
	if (cr == nil) || (len(cr.rules) == 0) || (ri == nil) || (!cr.hasBlockRules && !cr.hasTerminalRules) {
		return false, false
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

	blocked, terminal := rp.PropertyRules.checkRequestBlocked(ri)
	if blocked {
		return true
	}

	if terminal {
		return false
	}

	return rp.OrgRules.IsRequestBlocked(ri)
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

func (op *overrideProperty) applyMaxLevel(level int16, ruleID int32) {
	if op.level == nil || level > *op.level {
		op.level = &level
		op.ruleID = ruleID
	}
}

func (op *overrideProperty) applyMaxGrowth(growth dbgen.DifficultyGrowth, ruleID int32) {
	if op.growth == nil || growthOrder(growth) > growthOrder(*op.growth) {
		op.growth = &growth
		op.ruleID = ruleID
	}
}

// ruleBase contains common fields for all rule types
type ruleBase struct {
	ruleID   int32
	matcher  Matcher
	terminal bool
}

func (rb *ruleBase) Matches(ri *RequestInfo) bool { return rb.matcher.Matches(ri) }
func (rb *ruleBase) IsTerminal() bool             { return rb.terminal }

// difficultyLevelRule adjusts the difficulty level by a percentage for a property
type difficultyLevelRule struct {
	ruleBase
	percentDiff int16 // percentage difference (e.g., +20 means +20%, -20 means -20%)
}

func (r *difficultyLevelRule) Apply(op *overrideProperty) bool {
	baseLevel := op.base.Level()
	adjustedLevel := baseLevel + int16(int32(r.percentDiff)*(common.DifficultyDelta/2)/100)
	if adjustedLevel < int16(common.MinDifficultyLevel) {
		adjustedLevel = int16(common.MinDifficultyLevel)
	} else if adjustedLevel > int16(common.MaxDifficultyLevel) {
		adjustedLevel = int16(common.MaxDifficultyLevel)
	}
	op.applyMaxLevel(adjustedLevel, r.ruleID)
	return r.terminal
}

// difficultyGrowthRule overrides the difficulty growth for a property
type difficultyGrowthRule struct {
	ruleBase
	growth dbgen.DifficultyGrowth
}

func (r *difficultyGrowthRule) Apply(op *overrideProperty) bool {
	op.applyMaxGrowth(r.growth, r.ruleID)
	return r.terminal
}

// blockRequestRule blocks matching requests
type blockRequestRule struct {
	ruleBase
}

func (r *blockRequestRule) Apply(op *overrideProperty) bool {
	op.ruleID = r.ruleID
	return r.terminal
}

// breakRule stops processing following rules
type breakRule struct {
	ruleBase
}

func (r *breakRule) Apply(op *overrideProperty) bool {
	op.ruleID = r.ruleID
	return r.terminal
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
	switch rule.ConditionOperator {
	case dbgen.RuleConditionOperatorEquals,
		dbgen.RuleConditionOperatorContains,
		dbgen.RuleConditionOperatorEmpty,
		dbgen.RuleConditionOperatorIn:
	default:
		return nil, ErrUnsupportedConditionOperator
	}

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
	switch rule.ConditionOperator {
	case dbgen.RuleConditionOperatorEquals,
		dbgen.RuleConditionOperatorMatches,
		dbgen.RuleConditionOperatorIn,
		dbgen.RuleConditionOperatorEmpty:
	default:
		return nil, ErrUnsupportedConditionOperator
	}

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
		validCount := 0
		for _, item := range items {
			item = strings.TrimSpace(item)
			if len(item) == 0 {
				continue
			}
			validCount++
			if validCount > MaxIPAddressValues {
				return nil, ErrInvalidIPValue
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

func BuildHeaderMatcher(rule *dbgen.DifficultyRule) (Matcher, error) {
	switch rule.ConditionOperator {
	case dbgen.RuleConditionOperatorEquals,
		dbgen.RuleConditionOperatorIn:
	default:
		return nil, ErrUnsupportedConditionOperator
	}

	value := rule.ConditionValueStr.String
	hm := &HeaderMatcher{
		ConditionOperator:        rule.ConditionOperator,
		ConditionOperatorNegated: rule.ConditionOperatorNegated,
	}

	if rule.ConditionOperator == dbgen.RuleConditionOperatorIn {
		sep := defaultSeparator
		if rule.ConditionValueSeparator.Valid && len(rule.ConditionValueSeparator.String) > 0 {
			sep = rule.ConditionValueSeparator.String
		}
		items := strings.Split(value, sep)
		hm.ConditionValueItems = make([]string, 0, len(items))
		for _, item := range items {
			item = strings.TrimSpace(item)
			if len(item) == 0 {
				continue
			}
			hm.ConditionValueItems = append(hm.ConditionValueItems, http.CanonicalHeaderKey(item))
		}
	} else {
		hm.ConditionValueStr = http.CanonicalHeaderKey(value)
	}

	return hm, nil
}

// RulesCompiler compiles database rules into executable rule objects.
type RulesCompiler struct {
	uaParser  *useragent.Parser
	factories map[string]MatcherFactory
}

// NewRulesCompiler creates a new RulesCompiler with the provided user agent parser.
// The uaParser must not be nil; it is required for compiling user-agent based rules.
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
	rc.factories[string(dbgen.RuleConditionPropertyHTTPHeaderName)] = BuildHeaderMatcher
}

// RegisterMatcherFactory registers a MatcherFactory for the given condition property,
// replacing any existing factory for that property.
func (rc *RulesCompiler) RegisterMatcherFactory(property string, factory MatcherFactory) {
	if property == "" || factory == nil {
		return
	}

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
	if !ok || factory == nil {
		return nil, ErrUnknownConditionProperty
	}
	return factory(rule)
}

func (rc *RulesCompiler) CompileRule(ctx context.Context, dbRule *dbgen.DifficultyRule) (rule, error) {
	matcher, err := rc.buildMatcher(dbRule)
	if err != nil {
		return nil, err
	}

	base := ruleBase{ruleID: dbRule.ID, matcher: matcher, terminal: dbRule.Terminal}

	switch dbRule.ActionProperty {
	case dbgen.RuleActionPropertyDifficultyLevelPercent:
		percentDiff := max(-300, min(300, dbRule.ActionValue))
		return &difficultyLevelRule{
			ruleBase:    base,
			percentDiff: int16(percentDiff),
		}, nil
	case dbgen.RuleActionPropertyHTTPRequest:
		return &blockRequestRule{
			ruleBase: base,
		}, nil
	case dbgen.RuleActionPropertyDifficultyGrowth:
		return &difficultyGrowthRule{
			ruleBase: base,
			growth:   growthFromInt(dbRule.ActionValue),
		}, nil
	case dbgen.RuleActionPropertyBreak:
		return &breakRule{
			ruleBase: base,
		}, nil
	default:
		return nil, ErrUnknownActionProperty
	}
}

func (rc *RulesCompiler) Compile(ctx context.Context, dbRules []*dbgen.DifficultyRule) *CompiledRules {
	rules := make([]rule, 0, len(dbRules))

	sort.SliceStable(dbRules, func(i, j int) bool {
		return dbRules[i].Position < dbRules[j].Position
	})

	for _, r := range dbRules {
		if !r.Enabled {
			continue
		}
		// Zero percent difficulty rules have no effect and are treated like disabled.
		if r.ActionProperty == dbgen.RuleActionPropertyDifficultyLevelPercent && r.ActionValue == 0 {
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
