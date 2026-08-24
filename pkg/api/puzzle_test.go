package api

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	common_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	testPropertyDomain = "example.com"
)

func puzzleSuite(ctx context.Context, sitekey, domain string) (*http.Response, error) {
	return puzzleSuiteEx(ctx, http.MethodGet, sitekey, domain)
}

func puzzleSuiteEx(ctx context.Context, method, sitekey, domain string) (*http.Response, error) {
	slog.Log(ctx, common.LevelTrace, "Running puzzle suite", "domain", domain, "sitekey", sitekey)
	srv := http.NewServeMux()
	server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	//srv.HandleFunc("/", catchAll)

	req, err := http.NewRequest(method, "/"+common.PuzzleEndpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Origin", common_test.PrependProtocol(domain))
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

	q := req.URL.Query()
	q.Add(common.ParamSiteKey, sitekey)
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	return resp, nil
}

func randomUUID() *pgtype.UUID {
	eid := &pgtype.UUID{Valid: true}

	for i := range eid.Bytes {
		eid.Bytes[i] = byte(rand.Int())
	}

	return eid
}

func buildPuzzleRequest(t *testing.T, sitekey string) (*http.Request, *httptest.ResponseRecorder, *http.ServeMux) {
	t.Helper()

	srv := http.NewServeMux()
	server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	req, err := http.NewRequest(http.MethodGet, "/"+common.PuzzleEndpoint, nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

	q := req.URL.Query()
	q.Add(common.ParamSiteKey, sitekey)
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	return req, w, srv
}

func puzzleSuiteWithBackfillWait(t *testing.T, ctx context.Context, sitekey, domain string, waiter func()) {
	t.Helper()

	resp, err := puzzleSuite(ctx, sitekey, domain)
	if err != nil {
		t.Fatal(err)
	}

	// first request is successful, until we backfill
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	waiter()

	resp, err = puzzleSuite(ctx, sitekey, domain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}
}

func TestGetPuzzleWithoutAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	sitekey := db.UUIDToSiteKey(*randomUUID())
	ctx := t.Context()

	puzzleSuiteWithBackfillWait(t, ctx, sitekey, testPropertyDomain, func() {
		for i := 0; i < 10; i++ {
			time.Sleep(authBackfillDelay)

			if _, _, err := store.Impl().GetCachedPropertyBySitekey(ctx, sitekey); err != db.ErrCacheMiss {
				break
			} else {
				slog.DebugContext(ctx, "Waiting for property to be cached", "attempt", i, common.ErrAttr(err))
			}
		}
	})
}

func TestGetPuzzleWithoutSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := t.Context()

	user, org, err := db_tests.CreateNewBareAccount(ctx, store, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	if found := cache.Delete(ctx, db.PropertyBySitekeyCacheKey(sitekey)); !found {
		t.Fatal("property was not found in cache")
	}

	puzzleSuiteWithBackfillWait(t, ctx, sitekey, property.Domain, func() {
		// the reason we have this flaky delay is that otherwise we need access to
		// internal cache of user limiter in auth middleware (to check like WithoutAccount test does)
		time.Sleep(5 * authBackfillDelay)
	})
}

func TestGetPuzzleDisabledUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Parallel()

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	if err := db_tests.DisableUserForTest(ctx, store, user.ID); err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	if found := cache.Delete(ctx, db.PropertyBySitekeyCacheKey(sitekey)); !found {
		t.Fatal("property was not found in cache")
	}

	puzzleSuiteWithBackfillWait(t, ctx, sitekey, property.Domain, func() {
		time.Sleep(5 * authBackfillDelay)
	})
}

func parsePuzzle(resp *http.Response) (*puzzle.ComputePuzzle, string, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	responseStr := string(body)
	puzzleStr, _, _ := strings.Cut(responseStr, ".")
	decodedData, err := base64.StdEncoding.DecodeString(puzzleStr)
	if err != nil {
		return nil, "", err
	}

	p := new(puzzle.ComputePuzzle)
	err = p.UnmarshalBinary(decodedData)
	if err != nil {
		return nil, "", err
	}

	return p, responseStr, nil
}

func TestOptionsPuzzle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := puzzleSuiteEx(ctx, http.MethodOptions, db.UUIDToSiteKey(property.ExternalID), property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}
}

func TestGetPuzzle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := puzzleSuite(ctx, db.UUIDToSiteKey(property.ExternalID), property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	p, _, err := parsePuzzle(resp)
	if err != nil {
		t.Fatal(err)
	}

	if p.IsZero() {
		t.Errorf("Response puzzle is zero")
	}

	noticeHeader := resp.Header.Values(common.HeaderWidgetNotice)
	if len(noticeHeader) > 0 {
		t.Errorf("Expected no notice header, got %q", noticeHeader)
	}
}

func TestGetPuzzleWithFingerprintHeader(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	original := server.Verifier.FingerprintHeader
	server.Verifier.FingerprintHeader = "X-Fingerprint"
	defer func() { server.Verifier.FingerprintHeader = original }()

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	req, w, srv := buildPuzzleRequest(t, sitekey)
	req.Header.Set("Origin", common_test.PrependProtocol(property.Domain))
	req.Header.Set("X-Fingerprint", "test-fingerprint-value-12345")

	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	p, _, err := parsePuzzle(resp)
	if err != nil {
		t.Fatal(err)
	}

	if p.IsZero() {
		t.Error("Response puzzle is zero")
	}
}

func TestGetTestPuzzle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	resp, err := puzzleSuite(ctx, db.TestPropertySitekey, "localhost" /*domain*/)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	p, _, err := parsePuzzle(resp)
	if err != nil {
		t.Fatal(err)
	}

	if !p.IsZero() {
		t.Errorf("Test puzzle response is not zero puzzle")
	}
}

// setup is the same as for successful test, but we tombstone key in cache
func TestPuzzleCachePriority(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	err = cache.SetMissing(ctx, db.PropertyBySitekeyCacheKey(sitekey))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := puzzleSuite(ctx, sitekey, property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}
}

// Test puzzle endpoint with invalid origin (mismatched domain)
func TestGetPuzzleInvalidOrigin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	// Create property for one domain
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "correct-domain.com"), org)
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	// Ensure property is cached first
	_, err = store.Impl().RetrievePropertyBySitekey(ctx, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	// Try to access puzzle with wrong origin (different domain)
	resp, err := puzzleSuite(ctx, sitekey, "wrong-domain.com")
	if err != nil {
		t.Fatal(err)
	}

	// Should be forbidden because origin doesn't match property domain
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status forbidden for invalid origin, got %d", resp.StatusCode)
	}

	resp, err = puzzleSuite(ctx, sitekey, property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK for correct origin, got %d", resp.StatusCode)
	}
}

func TestGetPuzzleInvalidSitekeyLength(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Valid sitekey length is db.SitekeyLen. Truncate one character to make it invalid.
	validSitekey := db.UUIDToSiteKey(*randomUUID())
	truncatedSitekey := validSitekey[:len(validSitekey)-1]

	resp, err := puzzleSuite(ctx, truncatedSitekey, testPropertyDomain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status BadRequest for invalid sitekey length, got %d", resp.StatusCode)
	}
}

func TestOptionsPuzzleInvalidSitekeyTooShort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	validSitekey := db.UUIDToSiteKey(*randomUUID())
	truncatedSitekey := validSitekey[:len(validSitekey)-1]

	resp, err := puzzleSuiteEx(ctx, http.MethodOptions, truncatedSitekey, testPropertyDomain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status BadRequest for too-short sitekey, got %d", resp.StatusCode)
	}
}

func TestOptionsPuzzleInvalidSitekeyTooLong(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	validSitekey := db.UUIDToSiteKey(*randomUUID())
	extendedSitekey := validSitekey + "a"

	resp, err := puzzleSuiteEx(ctx, http.MethodOptions, extendedSitekey, testPropertyDomain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status BadRequest for too-long sitekey, got %d", resp.StatusCode)
	}
}

func TestGetPuzzleEmptyOriginWithReferer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	req, w, srv := buildPuzzleRequest(t, sitekey)
	req.Header.Set(common.HeaderReferer, common_test.PrependProtocol(property.Domain))

	srv.ServeHTTP(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK when Referer is set, got %d", resp.StatusCode)
	}
}

func TestGetPuzzleBothOriginAndRefererEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	req, w, srv := buildPuzzleRequest(t, sitekey)

	srv.ServeHTTP(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status BadRequest when both Origin and Referer are empty, got %d", resp.StatusCode)
	}
}

func TestGetPuzzleDisabledProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)

	// First verify the property works
	resp, err := puzzleSuite(ctx, sitekey, property.Domain)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK before disabling, got %d", resp.StatusCode)
	}

	// Now disable the property
	if err := db_tests.DisableProperty(ctx, store, property.ID); err != nil {
		t.Fatal(err)
	}

	// Clear cache and reload the property from DB so it has Enabled: false
	cache.Delete(ctx, db.PropertyBySitekeyCacheKey(sitekey))
	_, err = store.Impl().RetrievePropertyBySitekey(ctx, sitekey)
	if err != nil {
		t.Fatal(err)
	}

	// Now the puzzle request should be forbidden
	resp, err = puzzleSuite(ctx, sitekey, property.Domain)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status Forbidden for disabled property, got %d", resp.StatusCode)
	}
}

func TestRecaptchaVerifyHandlerInvalidFormData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
	}{
		{
			name:        "InvalidFormEncoding",
			body:        string([]byte{0xff, 0xfe}), // Invalid UTF-8 - no valid secret field
			contentType: common.ContentTypeURLEncoded,
			wantStatus:  http.StatusBadRequest, // Empty secret due to invalid form data
		},
		{
			name:        "FormTooLarge",
			body:        "response=" + strings.Repeat("a", maxSolutionsBodySize), // 9 + 262144 = 262153 bytes, exceeds 262144 limit
			contentType: common.ContentTypeURLEncoded,
			wantStatus:  http.StatusBadRequest, // Secret is empty due to body limit being hit during form parsing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/"+common.SiteVerifyEndpoint, strings.NewReader(tt.body))
			req.Header.Set(common.HeaderContentType, tt.contentType)
			req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestPCVerifyHandlerInvalidFormData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	apikey, _, err := store.Impl().CreateAPIKey(ctx, user, db_tests.CreateNewPuzzleAPIKeyParams(t.Name()+"-apikey", time.Now(), 1*time.Hour, 10.0))
	if err != nil {
		t.Fatal(err)
	}
	secret := db.UUIDToSecret(apikey.ExternalID)

	srv := http.NewServeMux()
	server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "EmptyBody",
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "BodyTooLarge",
			body:       strings.Repeat("a", maxSolutionsBodySize+1), // Over MaxBytesHandler limit
			wantStatus: http.StatusBadRequest,                       // ReadAll fails, returns bad request
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/"+common.VerifyEndpoint, strings.NewReader(tt.body))
			req.Header.Set(common.HeaderAPIKey, secret)
			req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, w.Code)
			}
		})
	}
}

func TestGetPuzzleWithNoticeHeader(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, testPropertyDomain), org)
	if err != nil {
		t.Fatal(err)
	}

	// Temporarily set the notice provider to return a test message
	prev := server.NoticeProvider
	server.NoticeProvider = &db_tests.StubNoticeProvider{Value: "test notice message"}
	defer func() { server.NoticeProvider = prev }()

	resp, err := puzzleSuite(ctx, db.UUIDToSiteKey(property.ExternalID), property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	if noticeHeader := resp.Header.Get(common.HeaderWidgetNotice); len(noticeHeader) > 0 {
		t.Errorf("Expected empty notice header without property flag, got %q", noticeHeader)
	}

	property.ShowNotice = true

	resp, err = puzzleSuite(ctx, db.UUIDToSiteKey(property.ExternalID), property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %d", resp.StatusCode)
	}

	if noticeHeader := resp.Header.Get(common.HeaderWidgetNotice); noticeHeader != "test notice message" {
		t.Errorf("Expected notice header %q, got %q", "test notice message", noticeHeader)
	}
}
