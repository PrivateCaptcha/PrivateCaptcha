package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/jackc/pgx/v5"
)

type browserVersionCacheQuerier struct {
	*db.QuerierStub
	arg      *dbgen.CreateCacheParams
	readKey  string
	readData []byte
	readErr  error
}

type browserVersionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f browserVersionRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type browserVersionErrorReader struct{}

func (browserVersionErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (q *browserVersionCacheQuerier) CreateCache(_ context.Context, arg *dbgen.CreateCacheParams) (int64, error) {
	if q.Error != nil {
		return 0, q.Error
	}
	q.arg = arg
	q.readData = arg.Value
	return 1, nil
}

func (q *browserVersionCacheQuerier) GetCachedByKey(_ context.Context, key string) ([]byte, error) {
	q.readKey = key
	if q.readData == nil && q.readErr == nil {
		return nil, pgx.ErrNoRows
	}
	return q.readData, q.readErr
}

func newBrowserVersionTestStore(q *browserVersionCacheQuerier) *db.BusinessStore {
	return db.NewBusinessWithQuerier(nil, q, db.NewStaticCache[db.CacheKey, any](10, &db.CacheMissingValue{}))
}

func newBrowserVersionTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	versions := map[string]string{
		"win":            "152.0.7977.42",
		"mac":            "151.0.7922.139",
		"linux":          "150.0.7871.186",
		"android":        "154.0.8000.10",
		"ios":            "155.0.8100.20",
		"firefox":        "153.0.4",
		"firefox-mobile": "154.0.1",
		"safari":         "26.6",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/firefox" {
			_ = json.NewEncoder(w).Encode(map[string]string{"LATEST_FIREFOX_VERSION": versions["firefox"]})
			return
		}
		if r.URL.Path == "/firefox-mobile" {
			_ = json.NewEncoder(w).Encode(map[string]string{"version": versions["firefox-mobile"]})
			return
		}
		if r.URL.Path == "/safari" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"references": map[string]any{
					"stable": map[string]string{"title": "Safari " + versions["safari"] + " Release Notes"},
				},
			})
			return
		}

		platform := strings.TrimPrefix(r.URL.Path, "/chrome/")
		version, ok := versions[platform]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"versions": []map[string]string{{"version": version}},
		})
	}))
}

func newFetchBrowserVersionsTestJob(querier *browserVersionCacheQuerier, server *httptest.Server) *FetchBrowserVersionsJob {
	job := NewFetchBrowserVersionsJob(newBrowserVersionTestStore(querier))
	job.Client = server.Client()
	job.ChromeURLTemplate = server.URL + "/chrome/{platform}"
	job.FirefoxURL = server.URL + "/firefox"
	job.FirefoxMobileURL = server.URL + "/firefox-mobile"
	job.SafariURL = server.URL + "/safari"
	return job
}

func TestBrowserVersionJobsRefreshVersionsAfterFetch(t *testing.T) {
	querier := &browserVersionCacheQuerier{QuerierStub: &db.QuerierStub{}}
	browserVersions := rules.NewBrowserVersions()
	fetchJob, refreshJob := NewBrowserVersionJobs(newBrowserVersionTestStore(querier), browserVersions)
	fetchBrowserVersionsJob, ok := fetchJob.(*FetchBrowserVersionsJob)
	if !ok {
		t.Fatalf("fetch browser versions job type = %T, want *FetchBrowserVersionsJob", fetchJob)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go common.RunPeriodicJob(ctx, refreshJob)

	snapshot := &BrowserVersionsSnapshot{Versions: validBrowserVersionRecords()}
	if err := fetchBrowserVersionsJob.processSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	key := rules.BrowserVersionKey{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}
	var major int
	var found bool
	for range 10 {
		time.Sleep(200 * time.Millisecond)
		major, found = browserVersions.Major(key)
		if found && major == 152 {
			return
		}
	}
	t.Fatalf("browser major = %d, %v, want 152, true", major, found)
}

func TestFetchBrowserVersionsJobStoresFullSnapshotAndTriggers(t *testing.T) {
	ts := newBrowserVersionTestServer(t)
	defer ts.Close()

	querier := &browserVersionCacheQuerier{
		QuerierStub: &db.QuerierStub{},
		readData:    browserVersionSnapshotData(t, validBrowserVersionRecords()),
	}
	trigger := make(chan struct{}, 1)
	job := newFetchBrowserVersionsTestJob(querier, ts)
	job.RefreshTrigger = trigger

	if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
		t.Fatal(err)
	}
	if querier.arg == nil {
		t.Fatal("browser versions were not stored")
	}
	if querier.arg.Key != BrowserVersionsCacheKey {
		t.Fatalf("cache key = %q, want %q", querier.arg.Key, BrowserVersionsCacheKey)
	}
	if querier.arg.Column3 != 7*24*time.Hour {
		t.Fatalf("cache TTL = %v, want 7 days", querier.arg.Column3)
	}

	var snapshot BrowserVersionsSnapshot
	if err := json.Unmarshal(querier.arg.Value, &snapshot); err != nil {
		t.Fatal(err)
	}
	want := validBrowserVersionRecords()
	if fmt.Sprint(snapshot.Versions) != fmt.Sprint(want) {
		t.Fatalf("stored versions = %+v, want %+v", snapshot.Versions, want)
	}
	select {
	case <-trigger:
	default:
		t.Fatal("refresh was not triggered after storing versions")
	}
}

func TestFetchBrowserVersionsJobStoresCoreSnapshotWhenSafariUnavailable(t *testing.T) {
	ts := newBrowserVersionTestServer(t)
	defer ts.Close()

	querier := &browserVersionCacheQuerier{
		QuerierStub: &db.QuerierStub{},
		readData:    browserVersionSnapshotData(t, validBrowserVersionRecords()),
	}
	job := newFetchBrowserVersionsTestJob(querier, ts)
	job.SafariURL = ts.URL + "/missing"

	if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
		t.Fatal(err)
	}
	var snapshot BrowserVersionsSnapshot
	if err := json.Unmarshal(querier.arg.Value, &snapshot); err != nil {
		t.Fatal(err)
	}
	want := validBrowserVersionRecords()[:9]
	if fmt.Sprint(snapshot.Versions) != fmt.Sprint(want) {
		t.Fatalf("stored versions = %+v, want core versions %+v", snapshot.Versions, want)
	}
	if strings.Contains(string(querier.arg.Value), "last_known_safari_versions") {
		t.Fatal("stored snapshot contains Safari-specific version history")
	}
}

func TestRefreshBrowserVersionsJobRemovesUnavailableSafariBaselines(t *testing.T) {
	querier := &browserVersionCacheQuerier{
		QuerierStub: &db.QuerierStub{},
		readData:    browserVersionSnapshotData(t, validBrowserVersionRecords()),
	}
	store := newBrowserVersionTestStore(querier)
	provider := rules.NewBrowserVersions()
	refreshJob := NewRefreshBrowserVersionsJob(store, provider)
	if err := refreshJob.RunOnce(t.Context(), refreshJob.NewParams()); err != nil {
		t.Fatal(err)
	}

	fetchJob := NewFetchBrowserVersionsJob(store)
	if err := fetchJob.processSnapshot(t.Context(), &BrowserVersionsSnapshot{Versions: validBrowserVersionRecords()[:9]}); err != nil {
		t.Fatal(err)
	}
	if err := refreshJob.RunOnce(t.Context(), refreshJob.NewParams()); err != nil {
		t.Fatal(err)
	}

	for _, platform := range []string{rules.PlatformMacOS, rules.PlatformIOS} {
		if _, ok := provider.Major(rules.BrowserVersionKey{Browser: rules.BrowserSafari, Platform: platform}); ok {
			t.Fatalf("Safari %s baseline remained active", platform)
		}
	}
	if major, ok := provider.Major(rules.BrowserVersionKey{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}); !ok || major != 152 {
		t.Fatalf("Chrome Windows baseline = %d, %v, want 152, true", major, ok)
	}
}

func TestFetchBrowserVersionsJobRejectsUnsafeOrInvalidCurrentSnapshot(t *testing.T) {
	tests := []struct {
		name    string
		version string
		data    []byte
		readErr error
		wantErr error
	}{
		{name: "Rollback", version: "160.0.0.0", wantErr: errBrowserVersionsResponse},
		{name: "LargeJump", version: "100.0.0.0", wantErr: errBrowserVersionsResponse},
		{name: "CacheReadFailure", readErr: errors.New("read failed"), wantErr: errBrowserVersionsStorage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newBrowserVersionTestServer(t)
			defer ts.Close()

			data := tt.data
			if data == nil {
				records := validBrowserVersionRecords()
				if tt.version != "" {
					records[0].Version = tt.version
				}
				data = browserVersionSnapshotData(t, records)
			}
			querier := &browserVersionCacheQuerier{
				QuerierStub: &db.QuerierStub{},
				readData:    data,
				readErr:     tt.readErr,
			}
			job := newFetchBrowserVersionsTestJob(querier, ts)

			if err := job.RunOnce(t.Context(), job.NewParams()); err != tt.wantErr {
				t.Fatalf("RunOnce error = %v, want %v", err, tt.wantErr)
			}
			if querier.arg != nil {
				t.Fatal("unsafe browser versions were stored")
			}
		})
	}
}

func TestFetchBrowserVersionsJobReplacesLegacyDesktopSnapshot(t *testing.T) {
	ts := newBrowserVersionTestServer(t)
	defer ts.Close()

	querier := &browserVersionCacheQuerier{
		QuerierStub: &db.QuerierStub{},
		readData:    browserVersionSnapshotData(t, legacyBrowserVersionRecords()),
	}
	job := newFetchBrowserVersionsTestJob(querier, ts)

	if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
		t.Fatal(err)
	}
	if querier.arg == nil {
		t.Fatal("legacy browser versions were not replaced")
	}
}

func TestFetchBrowserVersionsJobDoesNotStorePartialSnapshot(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/firefox" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"versions":[{"version":"152.0.1.2"}]}`))
	}))
	defer ts.Close()

	querier := &browserVersionCacheQuerier{QuerierStub: &db.QuerierStub{}}
	trigger := make(chan struct{}, 1)
	job := newFetchBrowserVersionsTestJob(querier, ts)
	job.RefreshTrigger = trigger

	if err := job.RunOnce(t.Context(), job.NewParams()); err == nil {
		t.Fatal("expected Firefox fetch failure")
	}
	if querier.arg != nil {
		t.Fatal("partial browser versions were stored")
	}
	select {
	case <-trigger:
		t.Fatal("refresh was triggered after fetch failure")
	default:
	}
}

func TestFetchBrowserVersionsJobDoesNotTriggerAfterStoreFailure(t *testing.T) {
	ts := newBrowserVersionTestServer(t)
	defer ts.Close()

	storeErr := errors.New("store failed")
	querier := &browserVersionCacheQuerier{QuerierStub: &db.QuerierStub{Error: storeErr}}
	trigger := make(chan struct{}, 1)
	job := newFetchBrowserVersionsTestJob(querier, ts)
	job.RefreshTrigger = trigger

	if err := job.RunOnce(t.Context(), job.NewParams()); err != errBrowserVersionsStorage {
		t.Fatalf("RunOnce error = %v, want %v", err, errBrowserVersionsStorage)
	}
	select {
	case <-trigger:
		t.Fatal("refresh was triggered after store failure")
	default:
	}
}

func TestFetchBrowserVersionsJobRejectsInvalidSourceData(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "MalformedJSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"versions":`))
			},
		},
		{
			name: "OversizedResponse",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"versions":[{"version":"` + strings.Repeat("1", browserVersionsMaxBytes) + `"}]}`))
			},
		},
		{
			name: "MissingVersion",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/firefox" {
					_, _ = w.Write([]byte(`{"LATEST_FIREFOX_VERSION":""}`))
					return
				}
				_, _ = w.Write([]byte(`{"versions":[{"version":"152.0.1.2"}]}`))
			},
		},
		{
			name: "MultipleChromeVersions",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"versions":[{"version":"152.0.1.2"},{"version":"152.0.1.2"}]}`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.handler)
			defer ts.Close()

			querier := &browserVersionCacheQuerier{QuerierStub: &db.QuerierStub{}}
			job := newFetchBrowserVersionsTestJob(querier, ts)

			if err := job.RunOnce(t.Context(), job.NewParams()); err == nil {
				t.Fatal("expected invalid response error")
			}
			if querier.arg != nil {
				t.Fatal("invalid browser versions were stored")
			}
		})
	}
	t.Run("Redirect", func(t *testing.T) {
		job := NewFetchBrowserVersionsJob(nil)
		if err := job.Client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
			t.Fatalf("redirect error = %v, want %v", err, http.ErrUseLastResponse)
		}
	})
}

func TestFetchBrowserVersionsJobCoalescesRefreshTriggers(t *testing.T) {
	ts := newBrowserVersionTestServer(t)
	defer ts.Close()

	querier := &browserVersionCacheQuerier{QuerierStub: &db.QuerierStub{}}
	trigger := make(chan struct{}, 1)
	trigger <- struct{}{}
	job := newFetchBrowserVersionsTestJob(querier, ts)
	job.RefreshTrigger = trigger

	done := make(chan error, 1)
	go func() { done <- job.RunOnce(t.Context(), job.NewParams()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunOnce blocked on a full refresh trigger")
	}
}

func TestFetchBrowserVersionsJobRetryPolicy(t *testing.T) {
	if timeout := NewFetchBrowserVersionsJob(nil).Timeout(); timeout != 10*time.Minute {
		t.Fatalf("Timeout() = %v, want 10 minutes", timeout)
	}

	t.Run("TransientStatus", func(t *testing.T) {
		var attempts atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) == 1 {
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"versions":[{"version":"152.0.1.2"}]}`))
		}))
		defer ts.Close()

		job := NewFetchBrowserVersionsJob(nil)
		job.Client = ts.Client()
		job.ChromeURLTemplate = ts.URL + "/{platform}"
		version, err := job.fetchChromeVersion(t.Context(), chromePlatformWindows)
		if err != nil {
			t.Fatal(err)
		}
		if version != "152.0.1.2" || attempts.Load() != 2 {
			t.Fatalf("version = %q after %d attempts", version, attempts.Load())
		}
	})

	t.Run("TransportFailure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"versions":[{"version":"152.0.1.2"}]}`))
		}))
		defer ts.Close()

		var attempts atomic.Int32
		transport := ts.Client().Transport
		job := NewFetchBrowserVersionsJob(nil)
		job.Client = &http.Client{Transport: browserVersionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if attempts.Add(1) == 1 {
				return nil, errors.New("transport failed")
			}
			return transport.RoundTrip(r)
		})}
		job.ChromeURLTemplate = ts.URL + "/{platform}"
		version, err := job.fetchChromeVersion(t.Context(), chromePlatformWindows)
		if err != nil {
			t.Fatal(err)
		}
		if version != "152.0.1.2" || attempts.Load() != 2 {
			t.Fatalf("version = %q attempts = %d", version, attempts.Load())
		}
	})

	t.Run("PermanentStatus", func(t *testing.T) {
		var attempts atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			http.Error(w, "bad request", http.StatusBadRequest)
		}))
		defer ts.Close()

		job := NewFetchBrowserVersionsJob(nil)
		job.Client = ts.Client()
		job.ChromeURLTemplate = ts.URL + "/{platform}"
		if _, err := job.fetchChromeVersion(t.Context(), chromePlatformWindows); err != errBrowserVersionsResponse {
			t.Fatalf("fetch error = %v, want %v", err, errBrowserVersionsResponse)
		}
		if attempts.Load() != 1 {
			t.Fatalf("attempts = %d, want 1", attempts.Load())
		}
	})
}

func TestFetchSafariVersionSelectsLatestStableRelease(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"references": {
				"old": {"title": "Safari 18.6 Release Notes"},
				"current": {"title": "Safari 26.6 Release Notes"},
				"beta": {"title": "Safari 27 Beta Release Notes"},
				"other": {"title": "Safari Technology Preview Release Notes"}
			}
		}`))
	}))
	defer ts.Close()

	job := NewFetchBrowserVersionsJob(nil)
	job.Client = ts.Client()
	job.SafariURL = ts.URL
	version, err := job.fetchSafariVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if version != "26.6" {
		t.Fatalf("version = %q, want 26.6", version)
	}
}

func TestFetchSafariVersionIgnoresMajorOnlyReleasesWithoutLogging(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"references": {
				"major-only": {"title": "Safari 27 Release Notes"},
				"current": {"title": "Safari 26.6 Release Notes"},
				"malformed": {"title": "Safari invalid.version Release Notes"}
			}
		}`))
	}))
	defer ts.Close()

	var logs bytes.Buffer
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(logger) })

	job := NewFetchBrowserVersionsJob(nil)
	job.Client = ts.Client()
	job.SafariURL = ts.URL
	version, err := job.fetchSafariVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if version != "27" {
		t.Fatalf("version = %q, want 27", version)
	}
	if logs.Len() != 0 {
		t.Fatalf("fetching valid Safari releases logged an error: %s", logs.String())
	}
}

func TestFetchBrowserVersionsJobSkipsMalformedBrowserVersions(t *testing.T) {
	tests := []struct {
		name            string
		malformedSource string
		wantRecords     int
		missing         []rules.BrowserVersionKey
	}{
		{
			name:            "Chrome",
			malformedSource: "/chrome/win",
			wantRecords:     10,
			missing:         []rules.BrowserVersionKey{{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}},
		},
		{
			name:            "Firefox",
			malformedSource: "/firefox",
			wantRecords:     8,
			missing: []rules.BrowserVersionKey{
				{Browser: rules.BrowserFirefox, Platform: rules.PlatformWindows},
				{Browser: rules.BrowserFirefox, Platform: rules.PlatformMacOS},
				{Browser: rules.BrowserFirefox, Platform: rules.PlatformLinux},
			},
		},
		{
			name:            "FirefoxMobile",
			malformedSource: "/firefox-mobile",
			wantRecords:     10,
			missing:         []rules.BrowserVersionKey{{Browser: rules.BrowserFirefox, Platform: rules.PlatformAndroid}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(logger)

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == tt.malformedSource {
					switch r.URL.Path {
					case "/firefox":
						_, _ = w.Write([]byte(`{"LATEST_FIREFOX_VERSION":"not-a-version"}`))
					case "/firefox-mobile":
						_, _ = w.Write([]byte(`{"version":"not-a-version"}`))
					default:
						_, _ = w.Write([]byte(`{"versions":[{"version":"not-a-version"}]}`))
					}
					return
				}
				switch r.URL.Path {
				case "/firefox":
					_, _ = w.Write([]byte(`{"LATEST_FIREFOX_VERSION":"153.0.4"}`))
				case "/firefox-mobile":
					_, _ = w.Write([]byte(`{"version":"154.0.1"}`))
				case "/safari":
					_, _ = w.Write([]byte(`{"references":{"stable":{"title":"Safari 26.6 Release Notes"}}}`))
				default:
					_, _ = w.Write([]byte(`{"versions":[{"version":"152.0.1.2"}]}`))
				}
			}))
			defer ts.Close()

			querier := &browserVersionCacheQuerier{QuerierStub: &db.QuerierStub{}}
			job := newFetchBrowserVersionsTestJob(querier, ts)
			if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
				t.Fatalf("RunOnce error = %v, want nil", err)
			}
			if querier.arg == nil {
				t.Fatal("valid browser versions were not stored")
			}

			var snapshot BrowserVersionsSnapshot
			if err := json.Unmarshal(querier.arg.Value, &snapshot); err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Versions) != tt.wantRecords {
				t.Fatalf("stored records = %d, want %d", len(snapshot.Versions), tt.wantRecords)
			}
			for _, record := range snapshot.Versions {
				if record.Version == "not-a-version" {
					t.Fatal("malformed browser version was stored")
				}
			}

			provider := rules.NewBrowserVersions()
			refreshJob := NewRefreshBrowserVersionsJob(newBrowserVersionTestStore(querier), provider)
			if err := refreshJob.RunOnce(t.Context(), refreshJob.NewParams()); err != nil {
				t.Fatalf("refresh error = %v, want nil", err)
			}
			if major, ok := provider.Major(rules.BrowserVersionKey{Browser: rules.BrowserChrome, Platform: rules.PlatformMacOS}); !ok || major != 152 {
				t.Fatalf("valid Chrome baseline = %d, %v, want 152, true", major, ok)
			}
			for _, key := range tt.missing {
				if _, ok := provider.Major(key); ok {
					t.Fatalf("malformed source baseline %+v was refreshed", key)
				}
			}
			if logs.Len() != 0 {
				t.Fatalf("ignoring a malformed browser version logged an error: %s", logs.String())
			}
		})
	}
}

func TestFetchBrowserVersionsJobPreservesCacheWhenAllVersionsAreMalformed(t *testing.T) {
	original := browserVersionSnapshotData(t, validBrowserVersionRecords())
	querier := &browserVersionCacheQuerier{
		QuerierStub: &db.QuerierStub{},
		readData:    original,
	}
	trigger := make(chan struct{}, 1)
	job := NewFetchBrowserVersionsJob(newBrowserVersionTestStore(querier))
	job.RefreshTrigger = trigger

	if err := job.processSnapshot(t.Context(), &BrowserVersionsSnapshot{}); err != nil {
		t.Fatalf("processSnapshot error = %v, want nil", err)
	}
	if querier.arg != nil {
		t.Fatal("empty snapshot overwrote cached browser versions")
	}
	if string(querier.readData) != string(original) {
		t.Fatal("cached browser versions changed")
	}
	select {
	case <-trigger:
		t.Fatal("refresh was triggered for an empty snapshot")
	default:
	}
}

func TestBrowserVersionRetriableStatus(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusTooEarly} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			if !isRetriableBrowserVersionStatus(status) {
				t.Fatalf("isRetriableBrowserVersionStatus(%d) = false, want true", status)
			}
		})
	}
}

func TestBrowserVersionRetryableStatusWinsOverBodyReadFailure(t *testing.T) {
	client := &http.Client{Transport: browserVersionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(browserVersionErrorReader{}),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}

	_, err := doFetchBrowserVersionData(t.Context(), client, "http://example.com")
	var retriable common.RetriableError
	if !errors.As(err, &retriable) {
		t.Fatalf("fetch error = %v, want retriable error", err)
	}
	if retriable.Unwrap() != errBrowserVersionsResponse {
		t.Fatalf("retriable error = %v, want %v", retriable.Unwrap(), errBrowserVersionsResponse)
	}
}

func validBrowserVersionRecords() []BrowserVersionRecord {
	return []BrowserVersionRecord{
		{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows, Version: "152.0.7977.42"},
		{Browser: rules.BrowserChrome, Platform: rules.PlatformMacOS, Version: "151.0.7922.139"},
		{Browser: rules.BrowserChrome, Platform: rules.PlatformLinux, Version: "150.0.7871.186"},
		{Browser: rules.BrowserChrome, Platform: rules.PlatformAndroid, Version: "154.0.8000.10"},
		{Browser: rules.BrowserChrome, Platform: rules.PlatformIOS, Version: "155.0.8100.20"},
		{Browser: rules.BrowserFirefox, Platform: rules.PlatformWindows, Version: "153.0.4"},
		{Browser: rules.BrowserFirefox, Platform: rules.PlatformMacOS, Version: "153.0.4"},
		{Browser: rules.BrowserFirefox, Platform: rules.PlatformLinux, Version: "153.0.4"},
		{Browser: rules.BrowserFirefox, Platform: rules.PlatformAndroid, Version: "154.0.1"},
		{Browser: rules.BrowserSafari, Platform: rules.PlatformMacOS, Version: "26.6"},
		{Browser: rules.BrowserSafari, Platform: rules.PlatformIOS, Version: "26.6"},
	}
}

func legacyBrowserVersionRecords() []BrowserVersionRecord {
	records := validBrowserVersionRecords()
	return append(append([]BrowserVersionRecord{}, records[:3]...), records[5:8]...)
}

func browserVersionSnapshotData(t *testing.T, records []BrowserVersionRecord) []byte {
	t.Helper()
	data, err := json.Marshal(BrowserVersionsSnapshot{Versions: records})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRefreshBrowserVersionsJobParsesAndReplacesSnapshot(t *testing.T) {
	querier := &browserVersionCacheQuerier{
		QuerierStub: &db.QuerierStub{},
		readData:    browserVersionSnapshotData(t, validBrowserVersionRecords()),
	}
	provider := rules.NewBrowserVersions()
	job := NewRefreshBrowserVersionsJob(newBrowserVersionTestStore(querier), provider)

	if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
		t.Fatal(err)
	}
	if querier.readKey != BrowserVersionsCacheKey {
		t.Fatalf("cache key = %q, want %q", querier.readKey, BrowserVersionsCacheKey)
	}

	want := map[rules.BrowserVersionKey]int{
		{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}:  152,
		{Browser: rules.BrowserChrome, Platform: rules.PlatformIOS}:      155,
		{Browser: rules.BrowserFirefox, Platform: rules.PlatformAndroid}: 154,
		{Browser: rules.BrowserSafari, Platform: rules.PlatformMacOS}:    26,
		{Browser: rules.BrowserSafari, Platform: rules.PlatformIOS}:      26,
	}
	for key, wantMajor := range want {
		major, ok := provider.Major(key)
		if !ok || major != wantMajor {
			t.Fatalf("Major(%+v) = %d, %v, want %d, true", key, major, ok, wantMajor)
		}
	}
}

func TestRefreshBrowserVersionsJobPreservesProviderOnFailure(t *testing.T) {
	t.Run("InitialCacheMiss", func(t *testing.T) {
		querier := &browserVersionCacheQuerier{QuerierStub: &db.QuerierStub{}, readErr: pgx.ErrNoRows}
		provider := rules.NewBrowserVersions()
		key := rules.BrowserVersionKey{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}
		provider.Replace(map[rules.BrowserVersionKey]int{key: 149})
		job := NewRefreshBrowserVersionsJob(newBrowserVersionTestStore(querier), provider)
		if err := job.RunOnce(t.Context(), job.NewParams()); err != nil {
			t.Fatalf("RunOnce error = %v, want nil", err)
		}
		if major, ok := provider.Major(key); !ok || major != 149 {
			t.Fatalf("Major(%+v) = %d, %v, want 149, true", key, major, ok)
		}
	})

	dbErr := errors.New("read failed")
	validRecords := validBrowserVersionRecords()
	tests := []struct {
		name string
		data []byte
		err  error
	}{
		{name: "DatabaseError", err: dbErr},
		{name: "MalformedJSON", data: []byte(`{"versions":`)},
		{name: "EmptyVersion", data: browserVersionSnapshotData(t, append([]BrowserVersionRecord{{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows}}, validRecords[1:]...))},
		{name: "ZeroMajorVersion", data: browserVersionSnapshotData(t, append([]BrowserVersionRecord{{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows, Version: "0.1"}}, validRecords[1:]...))},
		{name: "MalformedVersion", data: browserVersionSnapshotData(t, append([]BrowserVersionRecord{{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows, Version: "152.x.1"}}, validRecords[1:]...))},
		{name: "VersionOverflow", data: browserVersionSnapshotData(t, append([]BrowserVersionRecord{{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows, Version: "999999999999999999999.0"}}, validRecords[1:]...))},
		{name: "TooManyVersionComponents", data: browserVersionSnapshotData(t, append([]BrowserVersionRecord{{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows, Version: "152.0.1.2.3"}}, validRecords[1:]...))},
		{name: "ImplausibleMajorVersion", data: browserVersionSnapshotData(t, append([]BrowserVersionRecord{{Browser: rules.BrowserChrome, Platform: rules.PlatformWindows, Version: "1001.0"}}, validRecords[1:]...))},
		{name: "DuplicateRecord", data: browserVersionSnapshotData(t, append([]BrowserVersionRecord{validRecords[0]}, validRecords[:8]...))},
		{name: "UnknownRecord", data: browserVersionSnapshotData(t, append([]BrowserVersionRecord{{Browser: "unknown", Platform: rules.PlatformMacOS, Version: "18.0"}}, validRecords[1:]...))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := rules.NewBrowserVersions()
			lastKnownGood := make(map[rules.BrowserVersionKey]int, len(validRecords))
			for _, record := range validRecords {
				lastKnownGood[rules.BrowserVersionKey{Browser: record.Browser, Platform: record.Platform}] = 149
			}
			provider.Replace(lastKnownGood)

			querier := &browserVersionCacheQuerier{
				QuerierStub: &db.QuerierStub{},
				readData:    tt.data,
				readErr:     tt.err,
			}
			job := NewRefreshBrowserVersionsJob(newBrowserVersionTestStore(querier), provider)
			wantErr := errBrowserVersionsResponse
			if tt.err != nil {
				wantErr = errBrowserVersionsStorage
			}
			if err := job.RunOnce(t.Context(), job.NewParams()); err != wantErr {
				t.Fatalf("RunOnce error = %v, want %v", err, wantErr)
			}

			for key, wantMajor := range lastKnownGood {
				major, ok := provider.Major(key)
				if !ok || major != wantMajor {
					t.Fatalf("Major(%+v) after failure = %d, %v, want %d, true", key, major, ok, wantMajor)
				}
			}
			if _, ok := provider.Major(rules.BrowserVersionKey{Browser: "unknown", Platform: rules.PlatformMacOS}); ok {
				t.Fatal("failure introduced an unsupported browser version")
			}
		})
	}
}
