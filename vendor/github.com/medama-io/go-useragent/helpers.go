package useragent

import (
	"strings"

	"github.com/medama-io/go-useragent/agents"
	"github.com/medama-io/go-useragent/internal"
)

// Browser returns the browser name. If no browser is found, it returns an empty string.
func (ua UserAgent) Browser() agents.Browser {
	return ua.browser.GetMatchBrowser()
}

// OS returns the operating system name. If no OS is found, it returns an empty string.
func (ua UserAgent) OS() agents.OS {
	return ua.os.GetMatchOS()
}

// Device returns the device type as a string.
func (ua UserAgent) Device() agents.Device {
	return ua.device.GetMatchDevice()
}

// BrowserVersion returns the browser version. If no version is found, it returns an empty string.
func (ua UserAgent) BrowserVersion() string {
	return string(ua.version[:ua.versionIndex])
}

// BrowserVersionMajor returns the major version of the browser. If no version is found, it returns an empty string.
func (ua UserAgent) BrowserVersionMajor() string {
	if ua.versionIndex == 0 {
		return ""
	}

	version := ua.BrowserVersion()

	return strings.Split(version, ".")[0]
}

// BrowserVersionMinor returns the minor version of the browser. If no version is found, it returns an empty string.
func (ua UserAgent) BrowserVersionMinor() string {
	if ua.versionIndex == 0 {
		return ""
	}

	version := ua.BrowserVersion()

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return ""
	}

	return parts[1]
}

// BrowserVersionPatch returns the patch version of the browser. If no version is found, it returns an empty string.
//
// Note: The patch version may include a suffix (e.g., "1.2.3b").
func (ua UserAgent) BrowserVersionPatch() string {
	if ua.versionIndex == 0 {
		return ""
	}

	version := ua.BrowserVersion()

	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return ""
	}

	// Sometimes the patch version has a suffix, e.g. "1.2.3b".
	return strings.Join(parts[2:], ".")
}

// IsAndroidBrowser returns true if the user agent is an Android browser.
func (ua UserAgent) IsAndroidBrowser() bool {
	return ua.browser == internal.BrowserAndroid
}

// IsChrome returns true if the user agent is a Chrome browser.
func (ua UserAgent) IsChrome() bool {
	return ua.browser == internal.BrowserChrome
}

// IsEdge returns true if the user agent is an Edge browser.
func (ua UserAgent) IsEdge() bool {
	return ua.browser == internal.BrowserEdge
}

// IsFirefox returns true if the user agent is a Firefox browser.
func (ua UserAgent) IsFirefox() bool {
	return ua.browser == internal.BrowserFirefox
}

// IsIE returns true if the user agent is an Internet Explorer browser.
func (ua UserAgent) IsIE() bool {
	return ua.browser == internal.BrowserIE
}

// IsOpera returns true if the user agent is an Opera browser.
func (ua UserAgent) IsOpera() bool {
	return ua.browser == internal.BrowserOpera
}

// IsOperaMini returns true if the user agent is an Opera Mini browser.
func (ua UserAgent) IsOperaMini() bool {
	return ua.browser == internal.BrowserOperaMini
}

// IsSafari returns true if the user agent is a Safari browser.
func (ua UserAgent) IsSafari() bool {
	return ua.browser == internal.BrowserSafari
}

// IsVivaldi returns true if the user agent is a Vivaldi browser.
func (ua UserAgent) IsVivaldi() bool {
	return ua.browser == internal.BrowserVivaldi
}

// IsSamsungBrowser returns true if the user agent is a Samsung browser.
func (ua UserAgent) IsSamsungBrowser() bool {
	return ua.browser == internal.BrowserSamsung
}

// IsFalkon returns true if the user agent is a Falkon browser.
func (ua UserAgent) IsFalkon() bool {
	return ua.browser == internal.BrowserFalkon
}

// IsNintendoBrowser returns true if the user agent is a Nintendo browser.
func (ua UserAgent) IsNintendoBrowser() bool {
	return ua.browser == internal.BrowserNintendo
}

// IsYandexBrowser returns true if the user agent is a Yandex browser.
func (ua UserAgent) IsYandexBrowser() bool {
	return ua.browser == internal.BrowserYandex
}

// IsHuaweiBrowser returns true if the user agent is a Huawei browser.
func (ua UserAgent) IsHuaweiBrowser() bool {
	return ua.browser == internal.BrowserHuawei
}

// IsAndroidOS returns true if the user agent is an Android OS device.
func (ua UserAgent) IsAndroidOS() bool {
	return ua.os == internal.OSAndroid
}

// IsChromeOS returns true if the user agent is a Chrome OS device.
func (ua UserAgent) IsChromeOS() bool {
	return ua.os == internal.OSChromeOS
}

// IsIOS returns true if the user agent is an iOS device.
func (ua UserAgent) IsIOS() bool {
	return ua.os == internal.OSIOS
}

// IsLinux returns true if the user agent is a Linux device.
func (ua UserAgent) IsLinux() bool {
	return ua.os == internal.OSLinux
}

// IsOpenBSD returns true if the user agent is an OpenBSD device.
func (ua UserAgent) IsOpenBSD() bool {
	return ua.os == internal.OSOpenBSD
}

// IsMacOS returns true if the user agent is a macOS device.
func (ua UserAgent) IsMacOS() bool {
	return ua.os == internal.OSMacOS
}

// IsWindows returns true if the user agent is a Windows device.
func (ua UserAgent) IsWindows() bool {
	return ua.os == internal.OSWindows
}

// IsDesktop returns true if the user agent is a desktop browser.
func (ua UserAgent) IsDesktop() bool {
	return ua.device == internal.DeviceDesktop
}

// IsMobile returns true if the user agent is a mobile browser.
func (ua UserAgent) IsMobile() bool {
	return ua.device == internal.DeviceMobile
}

// IsTablet returns true if the user agent is a tablet browser.
func (ua UserAgent) IsTablet() bool {
	return ua.device == internal.DeviceTablet
}

// IsTV returns true if the user agent is a TV browser.
func (ua UserAgent) IsTV() bool {
	return ua.device == internal.DeviceTV
}

// IsBot returns true if the user agent is a bot.
func (ua UserAgent) IsBot() bool {
	return ua.device == internal.DeviceBot
}

// GetBrowser returns the browser name. If no browser is found, it returns an empty string.
//
// Deprecated: Use .Browser() instead.
func (ua UserAgent) GetBrowser() string {
	return string(ua.browser.GetMatchBrowser())
}

// GetOS returns the operating system name. If no OS is found, it returns an empty string.
//
// Deprecated: Use .OS() instead.
func (ua UserAgent) GetOS() string {
	return string(ua.os.GetMatchOS())
}

// GetDevice returns the device type as a string.
//
// Deprecated: Use .Device() instead.
func (ua UserAgent) GetDevice() string {
	return string(ua.device.GetMatchDevice())
}

// GetVersion returns the browser version. If no version is found, it returns an empty string.
//
// Deprecated: Use .BrowserVersion() instead.
func (ua UserAgent) GetVersion() string {
	return ua.BrowserVersion()
}

// GetMajorVersion returns the major version of the browser. If no version is found, it returns an empty string.
//
// Deprecated: Use .BrowserVersionMajor() instead.
func (ua UserAgent) GetMajorVersion() string {
	return ua.BrowserVersionMajor()
}
