package rules

import (
	"net/netip"
	"strings"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

// extractFunc extracts a string value from RequestInfo for matching
type extractFunc func(ri *RequestInfo) string

// baseMatcher contains the core matching logic without allocations
type baseMatcher struct {
	conditionProperty         dbgen.RuleConditionProperty
	conditionOperator         dbgen.RuleConditionOperator
	conditionOperatorNegated  bool
	conditionValueStr         string
	conditionValueLower       string  // Pre-lowercased for Contains/In operators
	conditionValueItems       []string // Pre-split items for In operator
	conditionValueIPPrefix    netip.Prefix
}

// extract returns the string value from RequestInfo based on the condition property
func (bm *baseMatcher) extract(ri *RequestInfo) string {
	switch bm.conditionProperty {
	case dbgen.RuleConditionPropertyUserAgent:
		return ri.UserAgent()
	case dbgen.RuleConditionPropertyCountryCode:
		return ri.CountryCode()
	default:
		return ""
	}
}

// matches performs the actual matching logic
func (bm *baseMatcher) matches(ri *RequestInfo) bool {
	var result bool
	
	switch bm.conditionProperty {
	case dbgen.RuleConditionPropertyUserAgent, dbgen.RuleConditionPropertyCountryCode:
		result = bm.matchString(ri)
	case dbgen.RuleConditionPropertyIPAddress:
		result = bm.matchIPAddress(ri)
	default:
		result = false
	}
	
	if bm.conditionOperatorNegated {
		return !result
	}
	return result
}

// matchString handles string-based matching
func (bm *baseMatcher) matchString(ri *RequestInfo) bool {
	switch bm.conditionOperator {
	case dbgen.RuleConditionOperatorEquals:
		return strings.EqualFold(bm.extract(ri), bm.conditionValueStr)
	case dbgen.RuleConditionOperatorContains:
		return strings.Contains(strings.ToLower(bm.extract(ri)), bm.conditionValueLower)
	case dbgen.RuleConditionOperatorEmpty:
		return len(bm.extract(ri)) == 0
	case dbgen.RuleConditionOperatorIn:
		v := strings.ToLower(bm.extract(ri))
		for _, item := range bm.conditionValueItems {
			if item == v {
				return true
			}
		}
		return false
	default:
		return strings.EqualFold(bm.extract(ri), bm.conditionValueStr)
	}
}

// matchIPAddress handles IP address matching
func (bm *baseMatcher) matchIPAddress(ri *RequestInfo) bool {
	ip := ri.IPAddr()
	
	switch bm.conditionOperator {
	case dbgen.RuleConditionOperatorEmpty:
		return !ip.IsValid()
	default:
		if !ip.IsValid() {
			return false
		}
		return bm.conditionValueIPPrefix.Contains(ip)
	}
}
