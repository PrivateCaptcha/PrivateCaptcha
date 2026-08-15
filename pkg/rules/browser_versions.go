package rules

import "sync"

const (
	BrowserChrome  = "chrome"
	BrowserFirefox = "firefox"

	PlatformWindows = "windows"
	PlatformMacOS   = "macos"
	PlatformLinux   = "linux"
	PlatformAndroid = "android"
	PlatformIOS     = "ios"
)

type BrowserVersionKey struct {
	Browser  string
	Platform string
}

type BrowserVersions struct {
	mu       sync.RWMutex
	versions map[BrowserVersionKey]int
}

func NewBrowserVersions() *BrowserVersions {
	return &BrowserVersions{versions: make(map[BrowserVersionKey]int)}
}

func (p *BrowserVersions) Major(key BrowserVersionKey) (int, bool) {
	if p == nil {
		return 0, false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	major, ok := p.versions[key]
	return major, ok
}

func (p *BrowserVersions) Replace(versions map[BrowserVersionKey]int) {
	replacement := make(map[BrowserVersionKey]int, len(versions))
	for key, major := range versions {
		replacement[key] = major
	}

	p.mu.Lock()
	p.versions = replacement
	p.mu.Unlock()
}
