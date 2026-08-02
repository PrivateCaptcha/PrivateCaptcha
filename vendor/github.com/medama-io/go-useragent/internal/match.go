package internal

import (
	"cmp"
	"slices"

	str "github.com/boyter/go-string"
	"github.com/medama-io/go-useragent/agents"
)

// matchMap is a map of user agent types to their matching strings.
// These are the tokens saved into the trie when populating it.
//
// Matching tokens are ordered by precedence. The first match is the
// most important match.
var matchMap = map[Match][]string{
	// Browsers
	BrowserChrome:    {"CriOS", string(agents.BrowserChrome)},
	BrowserEdge:      {"EdgiOS", string(agents.BrowserEdge), "Edg"},
	BrowserFirefox:   {"FxiOS", string(agents.BrowserFirefox)},
	BrowserIE:        {"MSIE", "Trident"},
	BrowserOpera:     {"OPiOS", "OPR", string(agents.BrowserOpera)},
	BrowserOperaMini: {"Mini"},
	BrowserSafari:    {string(agents.BrowserSafari), "AppleWebKit"},
	BrowserVivaldi:   {string(agents.BrowserVivaldi)},
	BrowserSamsung:   {"SamsungBrowser"},
	BrowserFalkon:    {string(agents.BrowserFalkon)},
	BrowserNintendo:  {"NintendoBrowser"},
	BrowserYandex:    {"YaBrowser"},
	BrowserHuawei:    {"HuaweiBrowser"},

	// Operating Systems
	OSAndroid:  {string(agents.OSAndroid)},
	OSChromeOS: {"CrOS"},
	OSIOS:      {"iPhone", "iPad", "iPod"},
	OSLinux:    {string(agents.OSLinux), "Ubuntu", "Fedora"},
	OSFreeBSD:  {string(agents.OSFreeBSD)},
	OSOpenBSD:  {string(agents.OSOpenBSD)},
	OSMacOS:    {"Macintosh"},
	OSWindows:  {"Windows NT", "WindowsNT"},

	// Devices
	DeviceDesktop:     {string(agents.DeviceDesktop), "Ubuntu", "Fedora"},
	DeviceMobile:      {string(agents.DeviceMobile)},
	TokenMobileDevice: {"ONEPLUS", "Huawei", "HTC", "Galaxy", "iPhone", "iPod", "Windows Phone", "WindowsPhone", "LG"},
	DeviceTablet:      {string(agents.DeviceTablet), "Touch", "iPad", "Nintendo Switch", "NintendoSwitch", "Kindle"},
	DeviceTV:          {string(agents.DeviceTV), "Large Screen", "LargeScreen", "Smart Display", "SmartDisplay", "PLAYSTATION", "PlayStation", "ADT-2", "ADT", "ADT-1", "CrKey", "Roku", "AFT", "Web0S", "WebS", "Nexus Player", "NexusPlayer", "Xbox", "XBOX", "Nintendo WiiU", "NintendoWiiU"},
	DeviceBot: {
		string(agents.DeviceBot),
		"HeadlessChrome",
		"bot",
		"Slurp",
		"LinkCheck",
		"QuickLook",
		"Haosou",
		"Yahoo Ad",
		"YahooAd",
		"Google",
		"Mediapartners",
		"Headless",
		"facebookexternalhit",
		"facebookcatalog",
		"Baidu",
		"Instagram",
		"Pinterest",
		"PageSpeedInsights",
		"WhatsApp",
		"PHP",
		"curl",
		"Go-http-client",
		"Gohttpclient",
		"Java",
		"Ruby",
		"Nodejs",
		"Dart",
		"PHPCurl",
		"PHPHTTP",
		"PHPSOAP",
		"amazon-QBusiness",
		"amazonQBusiness",

		// Python client prefixes
		"Python",
		"python",
		"PycURL",
		"aiohttp",
		"Pythonurllib",
	},

	// Version
	TokenVersion: {"Version"},
}

// MatchPrecedenceMap is a map of user agent types to their importance
// in determining what is the actual browser/device/OS being used.
//
// For example, Chrome user agents also contain the string "Safari" at
// the end of the user agent. This means that if we only check for
// "Safari" first, we will incorrectly determine the browser to be Safari
// instead of Chrome.
//
// By setting a precedence, we can determine which match is more important
// and use that as the final result.
var MatchPrecedenceMap = map[Match]uint8{
	// Browsers
	BrowserSafari:    1, // Is always at the end of a Chrome user agent.
	BrowserAndroid:   2,
	BrowserChrome:    3,
	BrowserFirefox:   4,
	BrowserIE:        5,
	BrowserOpera:     6,
	BrowserOperaMini: 7,
	BrowserEdge:      8,
	BrowserVivaldi:   9,
	BrowserSamsung:   10,
	BrowserSilk:      11,
	BrowserFalkon:    12,
	BrowserNintendo:  13,
	BrowserYandex:    14,
	BrowserHuawei:    15,

	// Operating Systems
	OSLinux:    1,
	OSAndroid:  2,
	OSIOS:      3,
	OSFreeBSD:  4,
	OSOpenBSD:  5,
	OSChromeOS: 5,
	OSMacOS:    6,
	OSWindows:  7,

	// Types
	DeviceDesktop:     1,
	DeviceMobile:      2,
	TokenMobileDevice: 3,
	DeviceTablet:      4,
	DeviceTV:          5,
	DeviceBot:         6,
}

// MatchResults contains the information from MatchTokenIndexes.
type MatchResults struct {
	Match     Match
	MatchType MatchType

	// Precedence value for each result type to determine which result should be overwritten.
	// Higher values overwrite lower values.
	Precedence uint8

	EndIndex int
}

// MatchTokenIndexes finds the start and end indexes of necessary tokens
// that match a known browser, device, or OS. This is used to determine
// when to insert a result value into the trie.
func MatchTokenIndexes(ua string) []MatchResults {
	results := make([]MatchResults, 0, len(matchMap))
	exists := make(map[Match]bool)

	for key, match := range matchMap {
		for _, m := range match {
			// Check if key match doesn't already exist in results.
			// This is to prevent duplicate matches in the trie.
			if exists[key] {
				continue
			}

			indexes := str.IndexAll(ua, m, -1)

			// Return the last match.
			if len(indexes) == 0 {
				continue
			}

			lastIndex := indexes[len(indexes)-1]

			// Add the match to the results.
			matchType := key.GetMatchType()
			results = append(results, MatchResults{EndIndex: lastIndex[1], Match: key, MatchType: matchType, Precedence: MatchPrecedenceMap[key]})
			exists[key] = true
		}
	}

	// Sort the results by EndIndex in descending order.
	// This allows us to determine the first matching token in the user agent
	// when we iterate over it when populating the trie.
	//
	// Some tokens may have the same EndIndex, so we need to sort by Match key
	// to make it deterministic.
	slices.SortFunc(results, func(a, b MatchResults) int {
		// Sort by EndIndex in descending order.
		result := cmp.Compare(b.EndIndex, a.EndIndex)
		if result != 0 {
			return result
		}

		// Ascending order.
		return cmp.Compare(a.Match, b.Match)
	})

	return results
}
