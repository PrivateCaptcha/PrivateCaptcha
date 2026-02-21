package rules

import (
	"net/netip"
	"strings"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/medama-io/go-useragent"
)

// Matcher is the interface that all specialized matchers implement.
// It can be used by external packages to implement custom matching logic.
type Matcher interface {
	Matches(ri *RequestInfo) bool
}

// StringMatcher handles string-based matching (UserAgent, CountryCode, Domain).
// Extractor may be set to a custom function to extract the value from RequestInfo;
// if nil, the default extraction based on ConditionProperty is used.
type StringMatcher struct {
	ConditionProperty        dbgen.RuleConditionProperty
	ConditionOperator        dbgen.RuleConditionOperator
	ConditionValueStr        string
	ConditionValueItems      []string // Pre-split items for In operator
	ConditionOperatorNegated bool
	Extractor                func(*RequestInfo) string
}

// extract returns the string value from RequestInfo based on the condition property
func (sm *StringMatcher) extract(ri *RequestInfo) string {
	if sm.Extractor != nil {
		return sm.Extractor(ri)
	}
	switch sm.ConditionProperty {
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

// Matches performs the actual matching logic
func (sm *StringMatcher) Matches(ri *RequestInfo) bool {
	var result bool

	switch sm.ConditionOperator {
	case dbgen.RuleConditionOperatorEquals:
		result = strings.EqualFold(sm.extract(ri), sm.ConditionValueStr)
	case dbgen.RuleConditionOperatorContains:
		result = containsCaseInsensitive(sm.extract(ri), sm.ConditionValueStr)
	case dbgen.RuleConditionOperatorEmpty:
		result = len(sm.extract(ri)) == 0
	case dbgen.RuleConditionOperatorIn:
		extractedValue := sm.extract(ri)
		for _, item := range sm.ConditionValueItems {
			if strings.EqualFold(item, extractedValue) {
				result = true
				break
			}
		}
	default:
		result = strings.EqualFold(sm.extract(ri), sm.ConditionValueStr)
	}

	if sm.ConditionOperatorNegated {
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

// IPMatcher handles IP address matching
type IPMatcher struct {
	ConditionOperator        dbgen.RuleConditionOperator
	ConditionValueIPPrefixes []netip.Prefix
	ConditionOperatorNegated bool
}

// Matches performs IP address matching
func (im *IPMatcher) Matches(ri *RequestInfo) bool {
	ip := ri.IPAddr()
	var result bool

	switch im.ConditionOperator {
	case dbgen.RuleConditionOperatorEmpty:
		result = !ip.IsValid()
	default:
		if ip.IsValid() {
			for _, prefix := range im.ConditionValueIPPrefixes {
				if prefix.Contains(ip) {
					result = true
					break
				}
			}
		}
	}

	if im.ConditionOperatorNegated {
		return !result
	}
	return result
}

// BotMatcher handles bot detection for user agent
type BotMatcher struct {
	UAParser                 *useragent.Parser
	ConditionOperatorNegated bool
}

// Matches returns true if the user agent is a known bot (or empty)
func (bm *BotMatcher) Matches(ri *RequestInfo) bool {
	ua := ri.UserAgent()
	result := len(ua) == 0 || bm.UAParser.Parse(ua).IsBot()

	if bm.ConditionOperatorNegated {
		return !result
	}
	return result
}
