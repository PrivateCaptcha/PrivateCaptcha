package rules

import "net/http"

// Rule is the interface implemented by all rule types.
type Rule interface {
	rule()
}

// blockRequestRule is a rule that blocks requests matching the given condition.
type blockRequestRule struct {
	condition func(r *http.Request) bool
}

func (r *blockRequestRule) rule() {}

// NewBlockRequestRule creates a new rule that blocks requests for which condition returns true.
func NewBlockRequestRule(condition func(r *http.Request) bool) Rule {
	return &blockRequestRule{condition: condition}
}

// CompiledRules holds a compiled set of rules with an optimization flag indicating
// whether any blockRequestRule exists, allowing IsRequestBlocked to exit early
// if no blocking rules are present.
type CompiledRules struct {
	rules                []Rule
	hasBlockRequestRules bool
}

// NewCompiledRules creates a CompiledRules from the given rules slice and sets
// the hasBlockRequestRules flag if any rule is of type blockRequestRule.
func NewCompiledRules(rules []Rule) CompiledRules {
	hasBlock := false
	for _, r := range rules {
		if _, ok := r.(*blockRequestRule); ok {
			hasBlock = true
			break
		}
	}
	return CompiledRules{
		rules:                rules,
		hasBlockRequestRules: hasBlock,
	}
}

// IsRequestBlocked returns true if any blockRequestRule in the compiled rules
// matches the given request. It uses the hasBlockRequestRules flag to skip
// iteration entirely when no block rules are present.
func (cr *CompiledRules) IsRequestBlocked(r *http.Request) bool {
	if !cr.hasBlockRequestRules {
		return false
	}
	for _, rule := range cr.rules {
		if br, ok := rule.(*blockRequestRule); ok {
			if br.condition(r) {
				return true
			}
		}
	}
	return false
}
