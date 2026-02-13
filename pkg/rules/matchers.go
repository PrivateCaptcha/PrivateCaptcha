package rules

import (
	"net/netip"
	"strings"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

// matcherFunc returns true when the request matches the rule condition
type matcherFunc func(ri *RequestInfo) bool

// stringMatcher creates a matcherFunc for a string value using a string extractor
func stringMatcher(extract func(ri *RequestInfo) string, value string, operator dbgen.RuleConditionOperator, separator string, negated bool) matcherFunc {
	var baseMatcher matcherFunc
	
	switch operator {
	case dbgen.RuleConditionOperatorEquals:
		baseMatcher = func(ri *RequestInfo) bool {
			return strings.EqualFold(extract(ri), value)
		}
	case dbgen.RuleConditionOperatorContains:
		lowerValue := strings.ToLower(value)
		baseMatcher = func(ri *RequestInfo) bool {
			return strings.Contains(strings.ToLower(extract(ri)), lowerValue)
		}
	case dbgen.RuleConditionOperatorEmpty:
		baseMatcher = func(ri *RequestInfo) bool {
			return len(extract(ri)) == 0
		}
	case dbgen.RuleConditionOperatorIn:
		sep := ","
		if len(separator) > 0 {
			sep = separator
		}
		items := strings.Split(value, sep)
		for i, item := range items {
			items[i] = strings.ToLower(strings.TrimSpace(item))
		}
		baseMatcher = func(ri *RequestInfo) bool {
			v := strings.ToLower(extract(ri))
			for _, item := range items {
				if item == v {
					return true
				}
			}
			return false
		}
	default:
		baseMatcher = func(ri *RequestInfo) bool {
			return strings.EqualFold(extract(ri), value)
		}
	}
	
	if negated {
		return func(ri *RequestInfo) bool {
			return !baseMatcher(ri)
		}
	}
	
	return baseMatcher
}

func ipAddressMatchesMatcher(prefix netip.Prefix, negated bool) matcherFunc {
	if negated {
		return func(ri *RequestInfo) bool {
			ip := ri.IPAddr()
			if !ip.IsValid() {
				return false
			}
			return !prefix.Contains(ip)
		}
	}
	
	return func(ri *RequestInfo) bool {
		ip := ri.IPAddr()
		if !ip.IsValid() {
			return false
		}
		return prefix.Contains(ip)
	}
}

func ipAddressEmptyMatcher(negated bool) matcherFunc {
	if negated {
		return func(ri *RequestInfo) bool {
			return ri.IPAddr().IsValid()
		}
	}
	
	return func(ri *RequestInfo) bool {
		return !ri.IPAddr().IsValid()
	}
}
