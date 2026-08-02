package agents

type (
	// Browser represents a browser name.
	Browser string
	// OS represents an operating system name.
	OS string
	// Device represents a device type.
	Device string
)

const (
	BrowserAndroid   Browser = "Android Browser"
	BrowserChrome    Browser = "Chrome"
	BrowserEdge      Browser = "Edge"
	BrowserFirefox   Browser = "Firefox"
	BrowserIE        Browser = "IE"
	BrowserOpera     Browser = "Opera"
	BrowserOperaMini Browser = "Opera Mini"
	BrowserSafari    Browser = "Safari"
	BrowserVivaldi   Browser = "Vivaldi"
	BrowserSilk      Browser = "Amazon Silk"
	BrowserSamsung   Browser = "Samsung Browser"
	BrowserFalkon    Browser = "Falkon"
	BrowserNintendo  Browser = "Nintendo Browser"
	BrowserYandex    Browser = "Yandex Browser"
	BrowserHuawei    Browser = "Huawei Browser"

	OSAndroid  OS = "Android"
	OSChromeOS OS = "ChromeOS"
	OSIOS      OS = "iOS"
	OSLinux    OS = "Linux"
	OSFreeBSD  OS = "FreeBSD"
	OSOpenBSD  OS = "OpenBSD"
	OSMacOS    OS = "MacOS"
	OSWindows  OS = "Windows"

	DeviceDesktop Device = "Desktop"
	DeviceMobile  Device = "Mobile"
	DeviceTablet  Device = "Tablet"
	DeviceTV      Device = "TV"
	DeviceBot     Device = "Bot"
)

func (b Browser) String() string {
	return string(b)
}

func (o OS) String() string {
	return string(o)
}

func (d Device) String() string {
	return string(d)
}
