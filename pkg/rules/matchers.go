package rules

import (
	"bytes"
	"encoding/gob"
	"net/netip"
	"strconv"
	"strings"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/medama-io/go-useragent"
	"github.com/medama-io/go-useragent/agents"
)

// Matcher is the interface that all specialized matchers implement.
type Matcher interface {
	Matches(ri *RequestInfo) bool
	IsStale() bool
}

// StringMatcher handles string-based matching (UserAgent, CountryCode, Domain).
type StringMatcher struct {
	ConditionProperty        dbgen.RuleConditionProperty
	ConditionOperator        dbgen.RuleConditionOperator
	ConditionValueStr        string
	ConditionValueItems      []string // Pre-split items for In operator
	ConditionOperatorNegated bool
}

var _ Matcher = (*StringMatcher)(nil)

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

func (sm *StringMatcher) IsStale() bool {
	return false
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
		result = false
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

var _ Matcher = (*IPMatcher)(nil)

func (im *IPMatcher) IsStale() bool {
	return false
}

func (im *IPMatcher) Matches(ri *RequestInfo) bool {
	ip := ri.IPAddr()
	var result bool

	switch im.ConditionOperator {
	case dbgen.RuleConditionOperatorEmpty:
		result = !ip.IsValid()
	case dbgen.RuleConditionOperatorEquals:
		if ip.IsValid() && len(im.ConditionValueIPPrefixes) == 1 {
			result = im.ConditionValueIPPrefixes[0].Addr() == ip
		}
	case dbgen.RuleConditionOperatorIn:
		if ip.IsValid() {
			for _, p := range im.ConditionValueIPPrefixes {
				if p.Addr() == ip {
					result = true
					break
				}
			}
		}
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
	ConditionValueItems      []string
	ConditionOperatorNegated bool
}

var _ Matcher = (*HeaderMatcher)(nil)

func (hm *HeaderMatcher) IsStale() bool {
	return false
}

func (hm *HeaderMatcher) Matches(ri *RequestInfo) bool {
	var result bool

	if hm.ConditionOperator == dbgen.RuleConditionOperatorIn {
		for _, name := range hm.ConditionValueItems {
			if ri.HasHeader(name) {
				result = true
				break
			}
		}
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

var _ Matcher = (*BotMatcher)(nil)

func (bm *BotMatcher) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(bm.ConditionOperatorNegated); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (bm *BotMatcher) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	return dec.Decode(&bm.ConditionOperatorNegated)
}

func (bm *BotMatcher) looksLikeBot(ri *RequestInfo) bool {
	ua := ri.UserAgent()
	if len(ua) == 0 {
		return true
	}

	if bm.UAParser == nil {
		return false
	}

	parsed := ri.ParsedUserAgent(bm.UAParser)

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

func (bm *BotMatcher) IsStale() bool {
	return bm.UAParser == nil
}

func (bm *BotMatcher) Matches(ri *RequestInfo) bool {
	result := bm.looksLikeBot(ri)

	if bm.ConditionOperatorNegated {
		return !result
	}
	return result
}

type BrowserVersionMatcher struct {
	UAParser                 *useragent.Parser
	BrowserVersions          *BrowserVersions
	Threshold                int32
	ConditionOperatorNegated bool
}

const browserVersionMaxUserAgentBytes = 4 * 1024

var _ Matcher = (*BrowserVersionMatcher)(nil)

func (m *BrowserVersionMatcher) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(m.Threshold); err != nil {
		return nil, err
	}
	if err := enc.Encode(m.ConditionOperatorNegated); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (m *BrowserVersionMatcher) GobDecode(data []byte) error {
	m.UAParser = nil
	m.BrowserVersions = nil
	dec := gob.NewDecoder(bytes.NewBuffer(data))
	if err := dec.Decode(&m.Threshold); err != nil {
		return err
	}
	return dec.Decode(&m.ConditionOperatorNegated)
}

func (m *BrowserVersionMatcher) IsStale() bool {
	return m.UAParser == nil || m.BrowserVersions == nil
}

func browserVersionKey(ua useragent.UserAgent) (BrowserVersionKey, bool) {
	var key BrowserVersionKey
	switch ua.Browser() {
	case agents.BrowserChrome:
		key.Browser = BrowserChrome
	case agents.BrowserFirefox:
		key.Browser = BrowserFirefox
	case agents.BrowserSafari:
		key.Browser = BrowserSafari
	default:
		return BrowserVersionKey{}, false
	}
	switch ua.OS() {
	case agents.OSWindows:
		if ua.Device() != agents.DeviceDesktop {
			return BrowserVersionKey{}, false
		}
		key.Platform = PlatformWindows
	case agents.OSMacOS:
		if ua.Device() != agents.DeviceDesktop {
			return BrowserVersionKey{}, false
		}
		key.Platform = PlatformMacOS
	case agents.OSLinux:
		if ua.Device() != agents.DeviceDesktop {
			return BrowserVersionKey{}, false
		}
		key.Platform = PlatformLinux
	case agents.OSAndroid:
		if ua.Device() != agents.DeviceMobile && ua.Device() != agents.DeviceTablet {
			return BrowserVersionKey{}, false
		}
		key.Platform = PlatformAndroid
	case agents.OSIOS:
		if (key.Browser != BrowserChrome && key.Browser != BrowserSafari) || (ua.Device() != agents.DeviceMobile && ua.Device() != agents.DeviceTablet) {
			return BrowserVersionKey{}, false
		}
		key.Platform = PlatformIOS
	default:
		return BrowserVersionKey{}, false
	}
	return key, true
}

func (m *BrowserVersionMatcher) Matches(ri *RequestInfo) bool {
	if ri == nil || m.IsStale() || m.Threshold <= 0 {
		return false
	}

	rawUserAgent := ri.UserAgent()
	if len(rawUserAgent) > browserVersionMaxUserAgentBytes {
		return false
	}
	ua := ri.ParsedUserAgent(m.UAParser)
	key, ok := browserVersionKey(*ua)
	if !ok {
		return false
	}
	requestMajor, err := strconv.Atoi(ua.BrowserVersionMajor())
	if err != nil || requestMajor <= 0 {
		return false
	}
	currentMajor, ok := m.BrowserVersions.Major(key)
	if !ok || currentMajor <= 0 {
		return false
	}
	result := currentMajor > requestMajor && currentMajor-requestMajor > int(m.Threshold)
	if m.ConditionOperatorNegated {
		return !result
	}
	return result
}

// AlwaysMatcher matches every request unconditionally.
type AlwaysMatcher struct{}

var _ Matcher = (*AlwaysMatcher)(nil)

func (am *AlwaysMatcher) IsStale() bool {
	return false
}

func (am *AlwaysMatcher) Matches(_ *RequestInfo) bool {
	return true
}
