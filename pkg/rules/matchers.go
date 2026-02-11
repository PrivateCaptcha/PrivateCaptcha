package rules

import (
	"net/netip"
	"strings"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

// matcherFunc returns true when the request matches the rule condition
type matcherFunc func(ri *RequestInfo) bool

// stringMatcher creates a matcherFunc for a string value using a string extractor
func stringMatcher(extract func(ri *RequestInfo) string, value string, operator dbgen.RuleConditionOperator, separator string) matcherFunc {
	switch operator {
	case dbgen.RuleConditionOperatorEquals:
		return func(ri *RequestInfo) bool {
			return strings.EqualFold(extract(ri), value)
		}
	case dbgen.RuleConditionOperatorContains:
		lowerValue := strings.ToLower(value)
		return func(ri *RequestInfo) bool {
			return strings.Contains(strings.ToLower(extract(ri)), lowerValue)
		}
	case dbgen.RuleConditionOperatorEmpty:
		return func(ri *RequestInfo) bool {
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
		return func(ri *RequestInfo) bool {
			v := strings.ToLower(extract(ri))
			for _, item := range items {
				if item == v {
					return true
				}
			}
			return false
		}
	default:
		return func(ri *RequestInfo) bool {
			return strings.EqualFold(extract(ri), value)
		}
	}
}

func ipAddressMatchesMatcher(prefix netip.Prefix) matcherFunc {
	return func(ri *RequestInfo) bool {
		ip := ri.IPAddr()
		if !ip.IsValid() {
			return false
		}
		return prefix.Contains(ip)
	}
}

var ipAddressEmptyMatcher matcherFunc = func(ri *RequestInfo) bool {
	return !ri.IPAddr().IsValid()
}
