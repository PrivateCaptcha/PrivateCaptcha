package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/jpillora/backoff"
)

const (
	BrowserVersionsCacheKey = "browser_versions"

	browserVersionsCacheTTL      = 7 * 24 * time.Hour
	browserVersionsMaxBytes      = 64 * 1024
	browserVersionsLogBytes      = 100
	browserVersionAttempts       = 8
	browserVersionMaxLength      = 64
	browserVersionMaxParts       = 4
	browserVersionMaxMajor       = 1000
	browserVersionMaxJump        = 10
	browserVersionMaxConcurrency = 2

	chromePlatformWindows = "win"
	chromePlatformMacOS   = "mac"
	chromePlatformLinux   = "linux"
	chromePlatformAndroid = "android"
	chromePlatformIOS     = "ios"

	defaultChromeVersionURL        = "https://versionhistory.googleapis.com/v1/chrome/platforms/{platform}/channels/stable/versions?pageSize=1&order_by=version%20desc"
	defaultFirefoxVersionURL       = "https://product-details.mozilla.org/1.0/firefox_versions.json"
	defaultFirefoxMobileVersionURL = "https://product-details.mozilla.org/1.0/mobile_versions.json"
	defaultSafariVersionURL        = "https://developer.apple.com/tutorials/data/documentation/safari-release-notes.json"
)

var (
	errBrowserVersionsRequest  = errors.New("browser version request error")
	errBrowserVersionsResponse = errors.New("browser version response error")
	errBrowserVersionsStorage  = errors.New("browser version storage error")
)

var coreBrowserVersionKeys = map[rules.BrowserVersionKey]struct{}{
	{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}:  {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformMacOS}:    {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformLinux}:    {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformAndroid}:  {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformIOS}:      {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformWindows}: {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformMacOS}:   {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformLinux}:   {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformAndroid}: {},
}

var supportedBrowserVersionKeys = map[rules.BrowserVersionKey]struct{}{
	{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}:  {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformMacOS}:    {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformLinux}:    {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformAndroid}:  {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformIOS}:      {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformWindows}: {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformMacOS}:   {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformLinux}:   {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformAndroid}: {},
	{Browser: rules.BrowserSafari, Platform: rules.PlatformMacOS}:    {},
	{Browser: rules.BrowserSafari, Platform: rules.PlatformIOS}:      {},
}

var legacyBrowserVersionKeys = map[rules.BrowserVersionKey]struct{}{
	{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}:  {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformMacOS}:    {},
	{Browser: rules.BrowserChrome, Platform: rules.PlatformLinux}:    {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformWindows}: {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformMacOS}:   {},
	{Browser: rules.BrowserFirefox, Platform: rules.PlatformLinux}:   {},
}

type BrowserVersionRecord struct {
	Browser  string `json:"browser"`
	Platform string `json:"platform"`
	Version  string `json:"version"`
}

type BrowserVersionsSnapshot struct {
	Versions []BrowserVersionRecord `json:"versions"`
}

func (s *BrowserVersionsSnapshot) addBrowserVersion(browser, version string, platforms ...string) {
	for _, platform := range platforms {
		s.Versions = append(s.Versions, BrowserVersionRecord{
			Browser:  browser,
			Platform: platform,
			Version:  version,
		})
	}
}

type browserVersionSource struct {
	browser   string
	platforms []string
	fetch     func(context.Context) (string, error)
}

type browserVersionResult struct {
	version string
	err     error
}

type FetchBrowserVersionsJob struct {
	Store             db.Implementor
	Client            *http.Client
	ChromeURLTemplate string
	FirefoxURL        string
	FirefoxMobileURL  string
	SafariURL         string
	TriggerCh         <-chan struct{}
	RefreshTrigger    chan<- struct{}
}

type RefreshBrowserVersionsJob struct {
	Store           db.Implementor
	BrowserVersions *rules.BrowserVersions
	TriggerCh       chan struct{}
}

var _ common.PeriodicJob = (*FetchBrowserVersionsJob)(nil)
var _ common.PeriodicJob = (*RefreshBrowserVersionsJob)(nil)

func NewFetchBrowserVersionsJob(store db.Implementor) *FetchBrowserVersionsJob {
	return &FetchBrowserVersionsJob{
		Store: store,
		Client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		TriggerCh:         make(chan struct{}, 1),
		RefreshTrigger:    make(chan struct{}, 1),
		ChromeURLTemplate: defaultChromeVersionURL,
		FirefoxURL:        defaultFirefoxVersionURL,
		FirefoxMobileURL:  defaultFirefoxMobileVersionURL,
		SafariURL:         defaultSafariVersionURL,
	}
}

func NewRefreshBrowserVersionsJob(store db.Implementor, versions *rules.BrowserVersions) *RefreshBrowserVersionsJob {
	return &RefreshBrowserVersionsJob{
		Store:           store,
		BrowserVersions: versions,
		TriggerCh:       make(chan struct{}, 1),
	}
}

func (j *FetchBrowserVersionsJob) Name() string             { return "fetch_browser_versions_job" }
func (j *FetchBrowserVersionsJob) Interval() time.Duration  { return 3 * time.Hour }
func (j *FetchBrowserVersionsJob) Timeout() time.Duration   { return 10 * time.Minute }
func (j *FetchBrowserVersionsJob) Jitter() time.Duration    { return 30 * time.Minute }
func (j *FetchBrowserVersionsJob) Trigger() <-chan struct{} { return j.TriggerCh }
func (j *FetchBrowserVersionsJob) NewParams() any           { return struct{}{} }

func (j *RefreshBrowserVersionsJob) Name() string             { return "refresh_browser_versions_job" }
func (j *RefreshBrowserVersionsJob) Interval() time.Duration  { return 6 * time.Hour }
func (j *RefreshBrowserVersionsJob) Timeout() time.Duration   { return time.Minute }
func (j *RefreshBrowserVersionsJob) Jitter() time.Duration    { return 10 * time.Minute }
func (j *RefreshBrowserVersionsJob) Trigger() <-chan struct{} { return j.TriggerCh }
func (j *RefreshBrowserVersionsJob) NewParams() any           { return struct{}{} }

func browserVersionResponsePrefix(data []byte) string {
	return string(data[:min(len(data), browserVersionsLogBytes)])
}

func isRetriableBrowserVersionStatus(status int) bool {
	return status >= http.StatusInternalServerError ||
		status == http.StatusTooManyRequests ||
		status == http.StatusRequestTimeout ||
		status == http.StatusTooEarly
}

func doFetchBrowserVersionData(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil || url == "" {
		slog.ErrorContext(ctx, "Invalid browser version request configuration", "URL", url)
		return nil, errBrowserVersionsRequest
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create browser version request", "URL", url, common.ErrAttr(err))
		return nil, errBrowserVersionsRequest
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch browser version", "URL", url, common.ErrAttr(err))
		return nil, common.NewRetriableError(errBrowserVersionsRequest)
	}
	defer resp.Body.Close()

	data, readErr := io.ReadAll(io.LimitReader(resp.Body, browserVersionsMaxBytes+1))
	attrs := []any{"URL", url, "code", resp.StatusCode, "response", browserVersionResponsePrefix(data)}
	if readErr != nil {
		attrs = append(attrs, common.ErrAttr(readErr))
	}
	if isRetriableBrowserVersionStatus(resp.StatusCode) {
		slog.ErrorContext(ctx, "Browser version server request failed", attrs...)
		return data, common.NewRetriableError(errBrowserVersionsResponse)
	}
	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "Browser version request failed", attrs...)
		return data, errBrowserVersionsResponse
	}
	if readErr != nil {
		slog.ErrorContext(ctx, "Failed to read browser version response", "URL", url, "code", resp.StatusCode, common.ErrAttr(readErr))
		return nil, errBrowserVersionsResponse
	}

	return data, nil
}

func fetchBrowserVersionData(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	b := &backoff.Backoff{
		Min:    time.Second,
		Max:    10 * time.Second,
		Factor: 2,
		Jitter: true,
	}

	var data []byte
	var err error
	for attempt := 0; attempt < browserVersionAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				slog.ErrorContext(ctx, "Browser version request backoff interrupted", "URL", url, common.ErrAttr(ctx.Err()))
				return data, errBrowserVersionsRequest
			case <-time.After(b.Duration()):
			}
		}

		data, err = doFetchBrowserVersionData(ctx, client, url)
		var retriable common.RetriableError
		if !errors.As(err, &retriable) {
			return data, err
		}
		err = retriable.Unwrap()
		slog.WarnContext(ctx, "Failed to fetch browser version", "URL", url, "attempt", attempt+1, common.ErrAttr(err))
	}

	return data, err
}

func fetchBrowserVersionResponse(ctx context.Context, client *http.Client, url string, target any) error {
	data, err := fetchBrowserVersionData(ctx, client, url)
	if err != nil {
		return err
	}
	if target == nil || len(data) == 0 || len(data) > browserVersionsMaxBytes {
		slog.ErrorContext(ctx, "Invalid browser version response size", "URL", url, "size", len(data))
		return errBrowserVersionsResponse
	}
	if err := json.Unmarshal(data, target); err != nil {
		slog.ErrorContext(ctx, "Failed to parse browser version response", "URL", url, "size", len(data), "response", browserVersionResponsePrefix(data), common.ErrAttr(err))
		return errBrowserVersionsResponse
	}
	return nil
}

func (j *FetchBrowserVersionsJob) fetchChromeVersion(ctx context.Context, platform string) (string, error) {
	var response struct {
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	url := strings.ReplaceAll(j.ChromeURLTemplate, "{platform}", platform)
	slog.DebugContext(ctx, "Fetching browser version", "browser", rules.BrowserChrome, "platform", platform)
	if err := fetchBrowserVersionResponse(ctx, j.Client, url, &response); err != nil {
		return "", err
	}
	if len(response.Versions) != 1 || response.Versions[0].Version == "" {
		slog.ErrorContext(ctx, "Invalid Chrome version response", "platform", platform, "versions", len(response.Versions))
		return "", errBrowserVersionsResponse
	}
	version := response.Versions[0].Version
	slog.DebugContext(ctx, "Fetched browser version", "browser", rules.BrowserChrome, "platform", platform, "version", version)
	return version, nil
}

func (j *FetchBrowserVersionsJob) fetchFirefoxVersion(ctx context.Context) (string, error) {
	var response struct {
		Latest string `json:"LATEST_FIREFOX_VERSION"`
	}
	slog.DebugContext(ctx, "Fetching browser version", "browser", rules.BrowserFirefox)
	if err := fetchBrowserVersionResponse(ctx, j.Client, j.FirefoxURL, &response); err != nil {
		return "", err
	}
	if response.Latest == "" {
		slog.ErrorContext(ctx, "Invalid Firefox version response")
		return "", errBrowserVersionsResponse
	}
	slog.DebugContext(ctx, "Fetched browser version", "browser", rules.BrowserFirefox, "version", response.Latest)
	return response.Latest, nil
}

func (j *FetchBrowserVersionsJob) fetchFirefoxMobileVersion(ctx context.Context) (string, error) {
	var response struct {
		Version string `json:"version"`
	}
	slog.DebugContext(ctx, "Fetching browser version", "browser", rules.BrowserFirefox, "platform", rules.PlatformAndroid)
	if err := fetchBrowserVersionResponse(ctx, j.Client, j.FirefoxMobileURL, &response); err != nil {
		return "", err
	}
	if response.Version == "" {
		slog.ErrorContext(ctx, "Invalid Firefox mobile version response")
		return "", errBrowserVersionsResponse
	}
	slog.DebugContext(ctx, "Fetched browser version", "browser", rules.BrowserFirefox, "platform", rules.PlatformAndroid, "version", response.Version)
	return response.Version, nil
}

func isNewerBrowserVersion(candidate, current string) bool {
	candidateParts := strings.Split(candidate, ".")
	currentParts := strings.Split(current, ".")
	for i := 0; i < max(len(candidateParts), len(currentParts)); i++ {
		candidatePart := 0
		if i < len(candidateParts) {
			candidatePart, _ = strconv.Atoi(candidateParts[i])
		}
		currentPart := 0
		if i < len(currentParts) {
			currentPart, _ = strconv.Atoi(currentParts[i])
		}
		if candidatePart != currentPart {
			return candidatePart > currentPart
		}
	}
	return false
}

func (j *FetchBrowserVersionsJob) fetchSafariVersion(ctx context.Context) (string, error) {
	var response struct {
		References map[string]struct {
			Title string `json:"title"`
		} `json:"references"`
	}
	slog.DebugContext(ctx, "Fetching browser version", "browser", rules.BrowserSafari)
	if err := fetchBrowserVersionResponse(ctx, j.Client, j.SafariURL, &response); err != nil {
		return "", err
	}

	const titlePrefix = "Safari "
	const titleSuffix = " Release Notes"
	latest := ""
	for _, reference := range response.References {
		version, ok := strings.CutPrefix(reference.Title, titlePrefix)
		if !ok {
			continue
		}
		version, ok = strings.CutSuffix(version, titleSuffix)
		if !ok || strings.ContainsAny(version, " \t\r\n") {
			continue
		}
		if _, err := parseBrowserVersionMajor(ctx, version); err != nil {
			continue
		}
		if latest == "" || isNewerBrowserVersion(version, latest) {
			latest = version
		}
	}
	if latest == "" {
		slog.ErrorContext(ctx, "Invalid Safari version response")
		return "", errBrowserVersionsResponse
	}
	slog.DebugContext(ctx, "Fetched browser version", "browser", rules.BrowserSafari, "version", latest)
	return latest, nil
}

func fetchBrowserVersionSources(ctx context.Context, sources []browserVersionSource) (*BrowserVersionsSnapshot, error) {
	results := make([]browserVersionResult, len(sources))
	sem := make(chan struct{}, browserVersionMaxConcurrency)
	var wg sync.WaitGroup

	for i, source := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()

			logger := slog.With("browser", source.browser)
			if len(source.platforms) == 1 {
				logger = logger.With("platform", source.platforms[0])
			}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				logger.ErrorContext(ctx, "Failed to acquire browser version fetch slot", common.ErrAttr(ctx.Err()))
				results[i].err = errBrowserVersionsRequest
				return
			}

			results[i].version, results[i].err = source.fetch(ctx)
			if results[i].err != nil {
				logger.ErrorContext(ctx, "Failed to fetch browser version", common.ErrAttr(results[i].err))
			}
		}()
	}

	wg.Wait()
	snapshot := &BrowserVersionsSnapshot{Versions: make([]BrowserVersionRecord, 0, len(supportedBrowserVersionKeys))}
	for i, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		snapshot.addBrowserVersion(sources[i].browser, result.version, sources[i].platforms...)
	}
	return snapshot, nil
}

func (j *FetchBrowserVersionsJob) fetchSnapshot(ctx context.Context) (*BrowserVersionsSnapshot, error) {
	platforms := []struct {
		api  string
		name string
	}{
		{api: chromePlatformWindows, name: rules.PlatformWindows},
		{api: chromePlatformMacOS, name: rules.PlatformMacOS},
		{api: chromePlatformLinux, name: rules.PlatformLinux},
		{api: chromePlatformAndroid, name: rules.PlatformAndroid},
		{api: chromePlatformIOS, name: rules.PlatformIOS},
	}

	sources := make([]browserVersionSource, 0, len(platforms)+2)
	for _, platform := range platforms {
		sources = append(sources, browserVersionSource{
			browser:   rules.BrowserChrome,
			platforms: []string{platform.name},
			fetch: func(ctx context.Context) (string, error) {
				return j.fetchChromeVersion(ctx, platform.api)
			},
		})
	}
	sources = append(sources,
		browserVersionSource{
			browser:   rules.BrowserFirefox,
			platforms: []string{rules.PlatformWindows, rules.PlatformMacOS, rules.PlatformLinux},
			fetch:     j.fetchFirefoxVersion,
		},
		browserVersionSource{
			browser:   rules.BrowserFirefox,
			platforms: []string{rules.PlatformAndroid},
			fetch:     j.fetchFirefoxMobileVersion,
		},
	)
	snapshot, err := fetchBrowserVersionSources(ctx, sources)
	if err != nil {
		return nil, err
	}
	safariVersion, err := j.fetchSafariVersion(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Safari version is unavailable", common.ErrAttr(err))
		return snapshot, nil
	}
	snapshot.addBrowserVersion(rules.BrowserSafari, safariVersion, rules.PlatformMacOS, rules.PlatformIOS)
	return snapshot, nil
}

func (j *FetchBrowserVersionsJob) processSnapshot(ctx context.Context, snapshot *BrowserVersionsSnapshot) error {
	if j.Store == nil {
		slog.ErrorContext(ctx, "Browser version store is not configured")
		return errBrowserVersionsStorage
	}
	majors, err := browserVersionMajors(ctx, snapshot)
	if err != nil {
		return err
	}
	if err := j.validateUpdate(ctx, majors); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to encode browser versions", common.ErrAttr(err))
		return errBrowserVersionsResponse
	}
	if err := j.Store.Impl().StoreInCache(ctx, BrowserVersionsCacheKey, data, browserVersionsCacheTTL); err != nil {
		slog.ErrorContext(ctx, "Failed to store browser versions", common.ErrAttr(err))
		return errBrowserVersionsStorage
	}

	common.TriggerNonBlocking(j.RefreshTrigger)

	return nil
}

func (j *FetchBrowserVersionsJob) validateUpdate(ctx context.Context, next map[rules.BrowserVersionKey]int) error {
	data, err := j.Store.Impl().RetrieveFromCache(ctx, BrowserVersionsCacheKey)
	if errors.Is(err, db.ErrCacheMiss) {
		return nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve current browser versions", common.ErrAttr(err))
		return errBrowserVersionsStorage
	}
	if len(data) == 0 {
		slog.ErrorContext(ctx, "Current browser version snapshot is empty")
		return errBrowserVersionsResponse
	}

	var snapshot BrowserVersionsSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		slog.ErrorContext(ctx, "Failed to decode current browser versions", "size", len(data), "response", browserVersionResponsePrefix(data), common.ErrAttr(err))
		return errBrowserVersionsResponse
	}
	current, err := currentBrowserVersionMajors(ctx, &snapshot)
	if err != nil {
		return err
	}
	for key, currentMajor := range current {
		nextMajor, ok := next[key]
		if !ok && key.Browser == rules.BrowserSafari {
			continue
		}
		if nextMajor < currentMajor || nextMajor-currentMajor > browserVersionMaxJump {
			slog.ErrorContext(ctx, "Unsafe browser version change", "browser", key.Browser, "platform", key.Platform, "current", currentMajor, "next", nextMajor)
			return errBrowserVersionsResponse
		}
	}
	return nil
}

func (j *FetchBrowserVersionsJob) RunOnce(ctx context.Context, _ any) error {
	snapshot, err := j.fetchSnapshot(ctx)
	if err == nil {
		err = j.processSnapshot(ctx, snapshot)
	}
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch browser versions", common.ErrAttr(err))
	}
	return err
}

func parseBrowserVersionMajor(ctx context.Context, version string) (int, error) {
	if len(version) == 0 || len(version) > browserVersionMaxLength {
		slog.ErrorContext(ctx, "Browser version length is invalid", "length", len(version))
		return 0, errBrowserVersionsResponse
	}
	parts := strings.Split(version, ".")
	if len(parts) < 2 || len(parts) > browserVersionMaxParts {
		slog.ErrorContext(ctx, "Browser version component count is invalid", "version", browserVersionResponsePrefix([]byte(version)), "components", len(parts))
		return 0, errBrowserVersionsResponse
	}

	major := 0
	for i, part := range parts {
		if part == "" {
			slog.ErrorContext(ctx, "Browser version has an empty component", "version", browserVersionResponsePrefix([]byte(version)), "component", i)
			return 0, errBrowserVersionsResponse
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				slog.ErrorContext(ctx, "Browser version has a non-numeric component", "version", browserVersionResponsePrefix([]byte(version)), "component", i)
				return 0, errBrowserVersionsResponse
			}
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			slog.ErrorContext(ctx, "Browser version component overflow", "version", browserVersionResponsePrefix([]byte(version)), "component", i, common.ErrAttr(err))
			return 0, errBrowserVersionsResponse
		}
		if i == 0 {
			major = value
		}
	}
	if major <= 0 || major > browserVersionMaxMajor {
		slog.ErrorContext(ctx, "Browser version major is implausible", "version", browserVersionResponsePrefix([]byte(version)), "major", major)
		return 0, errBrowserVersionsResponse
	}
	return major, nil
}

func browserVersionMajorsForKeys(ctx context.Context, snapshot *BrowserVersionsSnapshot, supportedKeys map[rules.BrowserVersionKey]struct{}) (map[rules.BrowserVersionKey]int, error) {
	if snapshot == nil || len(snapshot.Versions) != len(supportedKeys) {
		records := 0
		if snapshot != nil {
			records = len(snapshot.Versions)
		}
		slog.ErrorContext(ctx, "Browser version snapshot is incomplete", "records", records, "expected", len(supportedKeys))
		return nil, errBrowserVersionsResponse
	}

	versions := make(map[rules.BrowserVersionKey]int, len(snapshot.Versions))
	for _, record := range snapshot.Versions {
		key := rules.BrowserVersionKey{Browser: record.Browser, Platform: record.Platform}
		if _, ok := supportedKeys[key]; !ok {
			slog.ErrorContext(ctx, "Browser version is unsupported", "browser", record.Browser, "platform", record.Platform)
			return nil, errBrowserVersionsResponse
		}
		if _, ok := versions[key]; ok {
			slog.ErrorContext(ctx, "Browser version is duplicated", "browser", record.Browser, "platform", record.Platform)
			return nil, errBrowserVersionsResponse
		}
		major, err := parseBrowserVersionMajor(ctx, record.Version)
		if err != nil {
			return nil, err
		}
		versions[key] = major
	}
	return versions, nil
}

func browserVersionMajors(ctx context.Context, snapshot *BrowserVersionsSnapshot) (map[rules.BrowserVersionKey]int, error) {
	if snapshot != nil && len(snapshot.Versions) == len(coreBrowserVersionKeys) {
		return browserVersionMajorsForKeys(ctx, snapshot, coreBrowserVersionKeys)
	}
	return browserVersionMajorsForKeys(ctx, snapshot, supportedBrowserVersionKeys)
}

func currentBrowserVersionMajors(ctx context.Context, snapshot *BrowserVersionsSnapshot) (map[rules.BrowserVersionKey]int, error) {
	if snapshot != nil && len(snapshot.Versions) == len(legacyBrowserVersionKeys) {
		return browserVersionMajorsForKeys(ctx, snapshot, legacyBrowserVersionKeys)
	}
	return browserVersionMajors(ctx, snapshot)
}

func (j *RefreshBrowserVersionsJob) RunOnce(ctx context.Context, _ any) error {
	if j.Store == nil || j.BrowserVersions == nil {
		slog.ErrorContext(ctx, "Browser version refresh is not configured", "hasStore", j.Store != nil, "hasBrowserVersions", j.BrowserVersions != nil)
		return errBrowserVersionsStorage
	}

	data, err := j.Store.Impl().RetrieveFromCache(ctx, BrowserVersionsCacheKey)
	if errors.Is(err, db.ErrCacheMiss) {
		slog.WarnContext(ctx, "Browser versions are not cached", common.ErrAttr(err))
		return nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve cached browser versions", common.ErrAttr(err))
		return errBrowserVersionsStorage
	}
	var snapshot BrowserVersionsSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		slog.ErrorContext(ctx, "Failed to decode cached browser versions", "size", len(data), "response", browserVersionResponsePrefix(data), common.ErrAttr(err))
		return errBrowserVersionsResponse
	}
	versions, err := browserVersionMajors(ctx, &snapshot)
	if err != nil {
		return err
	}

	j.BrowserVersions.Replace(versions)
	return nil
}
