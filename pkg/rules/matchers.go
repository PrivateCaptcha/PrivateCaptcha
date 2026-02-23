package rules

import (
	"net/netip"
	"strings"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/medama-io/go-useragent"
	"github.com/medama-io/go-useragent/agents"
)

// Matcher is the interface that all specialized matchers implement.
type Matcher interface {
	Matches(ri *RequestInfo) bool
}

// StringMatcher handles string-based matching (UserAgent, CountryCode, Domain).
type StringMatcher struct {
	ConditionProperty        dbgen.RuleConditionProperty
	ConditionOperator        dbgen.RuleConditionOperator
	ConditionValueStr        string
	ConditionValueItems      []string // Pre-split items for In operator
	ConditionOperatorNegated bool
}

func (sm *StringMatcher) extract(ri *RequestInfo) string {
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

type HeaderMatcher struct {
	ConditionOperator        dbgen.RuleConditionOperator
	ConditionValueStr        string
	ConditionValueItems      []string
	ConditionOperatorNegated bool
}

func (hm *HeaderMatcher) Matches(ri *RequestInfo) bool {
	var result bool

	switch hm.ConditionOperator {
	case dbgen.RuleConditionOperatorEquals:
		result = ri.HasHeader(hm.ConditionValueStr)
	case dbgen.RuleConditionOperatorIn:
		for _, name := range hm.ConditionValueItems {
			if ri.HasHeader(name) {
				result = true
				break
			}
		}
	default:
		return false
	}

	if hm.ConditionOperatorNegated {
		return !result
	}
	return result
}

// BotMatcher handles bot detection for user agent
type BotMatcher struct {
	UAParser                 *useragent.Parser
	ConditionOperatorNegated bool
}

func (bm *BotMatcher) looksLikeBot(ua string) bool {
	if len(ua) == 0 {
		return true
	}

	parsed := bm.UAParser.Parse(ua)

	if parsed.IsBot() {
		return true
	}

	switch parsed.OS() {
	case "":
		return true
	case agents.OSWindows:
		for _, suspicious := range []string{"Windows 95", "Windows 98", "Windows CE", "Win 9x"} {
			if strings.Contains(ua, suspicious) {
				return true
			}
		}
	case agents.OSIOS:
		for _, suspicious := range []string{"iPod"} {
			if strings.Contains(ua, suspicious) {
				return true
			}
		}
	}

	return false
}

func (bm *BotMatcher) Matches(ri *RequestInfo) bool {
	result := bm.looksLikeBot(ri.UserAgent())

	if bm.ConditionOperatorNegated {
		return !result
	}
	return result
}
