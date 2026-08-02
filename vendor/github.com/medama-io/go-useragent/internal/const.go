package internal

import "github.com/medama-io/go-useragent/agents"

type (
	// Match is an enum for the browser, os or device name.
	Match uint8
	// MatchType is an enum for the match type.
	MatchType uint8
)

const (
	Unknown Match = iota

	// There is no match token for Android Browser, but the absence of any browser token paired with Android is a good indicator of this browser.
	BrowserAndroid
	BrowserChrome
	BrowserEdge
	BrowserFirefox
	BrowserIE
	BrowserOpera
	BrowserOperaMini
	BrowserSafari
	BrowserVivaldi
	BrowserSamsung
	BrowserSilk
	BrowserFalkon
	BrowserNintendo
	BrowserYandex
	BrowserHuawei

	OSAndroid
	OSChromeOS
	OSIOS
	OSLinux
	OSFreeBSD
	OSOpenBSD
	OSMacOS
	OSWindows

	DeviceDesktop
	DeviceMobile
	DeviceTablet
	DeviceTV
	DeviceBot

	TokenVersion
	// We need a separate type for mobile devices since some user agents use "Mobile/"
	// appended with a device ID. We need to handle these separately to strip those IDs
	// out.
	TokenMobileDevice

	MatchUnknown MatchType = iota
	MatchBrowser
	MatchOS
	MatchDevice
	MatchVersion
)

// GetMatchType returns the match type of a match result using the MatchPrecedenceMap.
func (m Match) GetMatchType() MatchType {
	switch m {
	case BrowserAndroid,
		BrowserChrome,
		BrowserEdge,
		BrowserFirefox,
		BrowserIE,
		BrowserOpera,
		BrowserOperaMini,
		BrowserSafari,
		BrowserVivaldi,
		BrowserSamsung,
		BrowserSilk,
		BrowserFalkon,
		BrowserNintendo,
		BrowserYandex,
		BrowserHuawei:
		return MatchBrowser

	case OSAndroid,
		OSChromeOS,
		OSIOS,
		OSLinux,
		OSFreeBSD,
		OSOpenBSD,
		OSMacOS,
		OSWindows:
		return MatchOS

	case DeviceDesktop,
		DeviceMobile,
		DeviceTablet,
		DeviceTV,
		DeviceBot,
		TokenMobileDevice:
		return MatchDevice

	case TokenVersion:
		return MatchVersion
	}

	return MatchUnknown
}

// GetMatchBrowser returns the browser name of a match.
func (m Match) GetMatchBrowser() agents.Browser {
	switch m {
	case BrowserAndroid:
		return agents.BrowserAndroid
	case BrowserChrome:
		return agents.BrowserChrome
	case BrowserEdge:
		return agents.BrowserEdge
	case BrowserFirefox:
		return agents.BrowserFirefox
	case BrowserIE:
		return agents.BrowserIE
	case BrowserOpera:
		return agents.BrowserOpera
	case BrowserOperaMini:
		return agents.BrowserOperaMini
	case BrowserSafari:
		return agents.BrowserSafari
	case BrowserVivaldi:
		return agents.BrowserVivaldi
	case BrowserSamsung:
		return agents.BrowserSamsung
	case BrowserSilk:
		return agents.BrowserSilk
	case BrowserFalkon:
		return agents.BrowserFalkon
	case BrowserNintendo:
		return agents.BrowserNintendo
	case BrowserYandex:
		return agents.BrowserYandex
	case BrowserHuawei:
		return agents.BrowserHuawei
	}

	return ""
}

// GetMatchOS returns the OS name of a match.
func (m Match) GetMatchOS() agents.OS {
	switch m {
	case OSAndroid:
		return agents.OSAndroid
	case OSChromeOS:
		return agents.OSChromeOS
	case OSIOS:
		return agents.OSIOS
	case OSLinux:
		return agents.OSLinux
	case OSOpenBSD:
		return agents.OSOpenBSD
	case OSMacOS:
		return agents.OSMacOS
	case OSWindows:
		return agents.OSWindows
	}

	return ""
}

// GetMatchDevice returns the device name of a match.
func (m Match) GetMatchDevice() agents.Device {
	switch m {
	case DeviceDesktop:
		return agents.DeviceDesktop
	case DeviceMobile:
		return agents.DeviceMobile
	case DeviceTablet:
		return agents.DeviceTablet
	case DeviceTV:
		return agents.DeviceTV
	case DeviceBot:
		return agents.DeviceBot
	}

	return ""
}

// GetMatchName returns the name of a match. This is used for debugging in tests.
func (m Match) GetMatchName() string {
	if browser := m.GetMatchBrowser(); browser != "" {
		return browser.String()
	}

	if os := m.GetMatchOS(); os != "" {
		return os.String()
	}

	if device := m.GetMatchDevice(); device != "" {
		return device.String()
	}

	switch m {
	case TokenVersion:
		return "Version"
	case TokenMobileDevice:
		return "MobileDevice"
	}

	return ""
}
