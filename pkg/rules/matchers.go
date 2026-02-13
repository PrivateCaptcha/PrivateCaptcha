package rules

import (
	"net/netip"
	"strings"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

// matcher is the interface that all specialized matchers implement
type matcher interface {
	matches(ri *RequestInfo) bool
}

// stringMatcher handles string-based matching (UserAgent, CountryCode)
type stringMatcher struct {
	conditionProperty    dbgen.RuleConditionProperty
	conditionOperator    dbgen.RuleConditionOperator
	conditionValueStr    string
	conditionValueItems  []string // Pre-split items for In operator
	conditionOperatorNegated bool
}

// extract returns the string value from RequestInfo based on the condition property
func (sm *stringMatcher) extract(ri *RequestInfo) string {
	switch sm.conditionProperty {
	case dbgen.RuleConditionPropertyUserAgent:
		return ri.UserAgent()
	case dbgen.RuleConditionPropertyCountryCode:
		return ri.CountryCode()
	default:
		return ""
	}
}

// matches performs the actual matching logic
func (sm *stringMatcher) matches(ri *RequestInfo) bool {
	var result bool
	
	switch sm.conditionOperator {
	case dbgen.RuleConditionOperatorEquals:
		result = strings.EqualFold(sm.extract(ri), sm.conditionValueStr)
	case dbgen.RuleConditionOperatorContains:
		result = containsCaseInsensitive(sm.extract(ri), sm.conditionValueStr)
	case dbgen.RuleConditionOperatorEmpty:
		result = len(sm.extract(ri)) == 0
	case dbgen.RuleConditionOperatorIn:
		extractedValue := sm.extract(ri)
		for _, item := range sm.conditionValueItems {
			if strings.EqualFold(item, extractedValue) {
				result = true
				break
			}
		}
	default:
		result = strings.EqualFold(sm.extract(ri), sm.conditionValueStr)
	}
	
	if sm.conditionOperatorNegated {
		return !result
	}
	return result
}

// containsCaseInsensitive checks if s contains substr in a case-insensitive manner
func containsCaseInsensitive(s, substr string) bool {
	// For contains, we need to check if substr appears anywhere in s
	// We'll iterate through s and check each position
	sLen := len(s)
	substrLen := len(substr)
	
	if substrLen == 0 {
		return true
	}
	if substrLen > sLen {
		return false
	}
	
	for i := 0; i <= sLen-substrLen; i++ {
		if strings.EqualFold(s[i:i+substrLen], substr) {
			return true
		}
	}
	return false
}

// ipMatcher handles IP address matching
type ipMatcher struct {
	conditionOperator        dbgen.RuleConditionOperator
	conditionValueIPPrefix   netip.Prefix
	conditionOperatorNegated bool
}

// matches performs IP address matching
func (im *ipMatcher) matches(ri *RequestInfo) bool {
	ip := ri.IPAddr()
	var result bool
	
	switch im.conditionOperator {
	case dbgen.RuleConditionOperatorEmpty:
		result = !ip.IsValid()
	default:
		if !ip.IsValid() {
			result = false
		} else {
			result = im.conditionValueIPPrefix.Contains(ip)
		}
	}
	
	if im.conditionOperatorNegated {
		return !result
	}
	return result
}
