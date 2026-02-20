package rules

import (
	"net/netip"
	"strings"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/medama-io/go-useragent"
)

// matcher is the interface that all specialized matchers implement
type matcher interface {
	matches(ri *RequestInfo) bool
}

// stringMatcher handles string-based matching (UserAgent, CountryCode)
type stringMatcher struct {
	conditionProperty        dbgen.RuleConditionProperty
	conditionOperator        dbgen.RuleConditionOperator
	conditionValueStr        string
	conditionValueItems      []string // Pre-split items for In operator
	conditionOperatorNegated bool
}

// extract returns the string value from RequestInfo based on the condition property
func (sm *stringMatcher) extract(ri *RequestInfo) string {
	switch sm.conditionProperty {
	case dbgen.RuleConditionPropertyUserAgent:
		return ri.UserAgent()
	case dbgen.RuleConditionPropertyCountryCode:
		return ri.CountryCode()
	case dbgen.RuleConditionPropertyDomain:
		return ri.Domain()
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
	sLen := len(s)
	substrLen := len(substr)

	if substrLen == 0 {
		return true
	}
	if substrLen > sLen {
		return false
	}

	// Convert to lowercase once and use standard Contains
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// ipMatcher handles IP address matching
type ipMatcher struct {
	conditionOperator        dbgen.RuleConditionOperator
	conditionValueIPPrefixes []netip.Prefix
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
		if ip.IsValid() {
			for _, prefix := range im.conditionValueIPPrefixes {
				if prefix.Contains(ip) {
					result = true
					break
				}
			}
		}
	}

	if im.conditionOperatorNegated {
		return !result
	}
	return result
}

// botMatcher handles bot detection for user agent
type botMatcher struct {
	uaParser                 *useragent.Parser
	conditionOperatorNegated bool
}

// matches returns true if the user agent is a known bot (or empty)
func (bm *botMatcher) matches(ri *RequestInfo) bool {
	ua := ri.UserAgent()
	result := len(ua) == 0 || bm.uaParser.Parse(ua).IsBot()

	if bm.conditionOperatorNegated {
		return !result
	}
	return result
}
