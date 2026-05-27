package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	common_test "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/jackc/pgx/v5/pgtype"
)

type stubSubmitFormURLVerifier struct {
	err  error
	urls []string
}

func (v *stubSubmitFormURLVerifier) VerifyURL(ctx context.Context, rawURL string) error {
	v.urls = append(v.urls, rawURL)
	return v.err
}

func (v *stubSubmitFormURLVerifier) VerifyResolvedAddress(ctx context.Context, host string, ip netip.Addr) error {
	return v.err
}

func (v *stubSubmitFormURLVerifier) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && (transport != nil) {
		return transport.DialContext(ctx, network, address)
	}

	panic("not configured")
}

type formProxyQuerierStub struct {
	*db.QuerierStub
	forms []*dbgen.Form
}

func (s *formProxyQuerierStub) GetFormsByExternalID(ctx context.Context, externalIDs []pgtype.UUID) ([]*dbgen.Form, error) {
	return s.forms, s.Error
}

func createFormProxyForTest(ctx context.Context, t *testing.T, name, domain string) (*dbgen.Form, *dbgen.Property) {
	t.Helper()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, name, testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, property, _, err := store.Impl().CreateNewForm(ctx, db_tests.CreateNewPropertyParams(user.ID, domain), &dbgen.CreateFormParams{
		Name:              t.Name(),
		URL:               "https://example.com/submit",
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	return form, property
}

func formProxySuite(t *testing.T, form *dbgen.Form, body url.Values) *http.Response {
	t.Helper()

	srv := http.NewServeMux()
	server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	req := httptest.NewRequest(http.MethodPost, "/"+common.FormEndpoint+"/"+db.UUIDToString(form.ExternalID), strings.NewReader(body.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Result()
}

func TestSubmitFormBatchSkipsUnsafeFormURL(t *testing.T) {
	downstreamCalled := false
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	expectedErr := errors.New("unsafe form URL")
	verifier := &stubSubmitFormURLVerifier{err: expectedErr}
	form := &dbgen.Form{ID: 1, ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 0, Enabled: true, Active: true}
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	store := db.NewBusinessWithQuerier(nil, &formProxyQuerierStub{QuerierStub: &db.QuerierStub{}, forms: []*dbgen.Form{form}}, cache)
	server := &Server{BusinessDB: store, FormURLVerifier: verifier, FormSubmitLogChan: make(chan *common.FormSubmitRecord, 1)}
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	if err := server.submitFormBatch(context.Background(), []*FormSubmission{submission}); err != nil {
		t.Fatalf("expected batch submission to continue after unsafe URL, got %v", err)
	}

	if len(verifier.urls) != 1 || verifier.urls[0] != downstream.URL {
		t.Fatalf("expected verifier to receive form URL, got %v", verifier.urls)
	}
	if downstreamCalled {
		t.Fatal("expected unsafe form URL to be skipped")
	}
	select {
	case record := <-server.FormSubmitLogChan:
		t.Fatalf("expected unsafe form URL not to record metrics, got %+v", record)
	default:
	}
}

func TestSubmitFormBatchRecordsFinalSuccess(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 2, Enabled: true, Active: true}
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	store := db.NewBusinessWithQuerier(nil, &formProxyQuerierStub{QuerierStub: &db.QuerierStub{}, forms: []*dbgen.Form{form}}, cache)
	server := &Server{BusinessDB: store, FormURLVerifier: &stubSubmitFormURLVerifier{}, FormSubmitLogChan: make(chan *common.FormSubmitRecord, 1)}
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	if err := server.submitFormBatch(context.Background(), []*FormSubmission{submission}); err != nil {
		t.Fatalf("expected batch submission to succeed, got %v", err)
	}

	select {
	case record := <-server.FormSubmitLogChan:
		if record.UserID != 7 || record.OrgID != 8 || record.FormID != 123 || record.Status != 0 {
			t.Fatalf("unexpected success record: %+v", record)
		}
	default:
		t.Fatal("expected success form metric record")
	}
}

func TestSubmitFormBatchRecordsOneFinalFailureAfterRetries(t *testing.T) {
	var attempts int
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 2, Enabled: true, Active: true}
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	store := db.NewBusinessWithQuerier(nil, &formProxyQuerierStub{QuerierStub: &db.QuerierStub{}, forms: []*dbgen.Form{form}}, cache)
	server := &Server{BusinessDB: store, FormURLVerifier: &stubSubmitFormURLVerifier{}, FormSubmitLogChan: make(chan *common.FormSubmitRecord, 2)}
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	if err := server.submitFormBatch(context.Background(), []*FormSubmission{submission}); err != nil {
		t.Fatalf("expected batch submission to continue after downstream failures, got %v", err)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 downstream attempts, got %d", attempts)
	}

	select {
	case record := <-server.FormSubmitLogChan:
		if record.UserID != 7 || record.OrgID != 8 || record.FormID != 123 || record.Status != 1 {
			t.Fatalf("unexpected failure record: %+v", record)
		}
	default:
		t.Fatal("expected failure form metric record")
	}

	select {
	case record := <-server.FormSubmitLogChan:
		t.Fatalf("expected one final failure record, got extra %+v", record)
	default:
	}
}

func TestSubmitFormReturnsSuccessResult(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 0, Enabled: true, Active: true}
	client := common.NewFormHTTPClient(&stubSubmitFormURLVerifier{})
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitForm(context.Background(), client, form, submission)
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.Success {
		t.Fatal("expected success result")
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
}

func TestSubmitFormReturnsFailureResult(t *testing.T) {
	var attempts int
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 0, Enabled: true, Active: true}
	client := common.NewFormHTTPClient(&stubSubmitFormURLVerifier{})
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitForm(context.Background(), client, form, submission)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Success {
		t.Fatal("expected failure result")
	}
	if result.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, result.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestSubmitFormReturnsFailureResultAfterRetries(t *testing.T) {
	var attempts int
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 2, Enabled: true, Active: true}
	client := common.NewFormHTTPClient(&stubSubmitFormURLVerifier{})
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitForm(context.Background(), client, form, submission)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Success {
		t.Fatal("expected failure result")
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, result.StatusCode)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestFormHTTPClientRejectsUnsafeRedirect(t *testing.T) {
	expectedErr := errors.New("unsafe redirect URL")
	verifier := &stubSubmitFormURLVerifier{err: expectedErr}
	client := common.NewFormHTTPClient(verifier)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/form", nil)

	err := client.CheckRedirect(req, nil)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected redirect verifier error, got %v", err)
	}
	if len(verifier.urls) != 1 || verifier.urls[0] != "http://127.0.0.1/form" {
		t.Fatalf("expected redirect URL to be verified, got %v", verifier.urls)
	}
}

func TestFormDialContextRejectsUnsafeResolvedAddress(t *testing.T) {
	verifier := NewFormURLVerifier()
	_, err := verifier.DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected localhost dial target to be rejected")
	}

	_, err = verifier.DialContext(context.Background(), "tcp", "[::1]:80")
	if err == nil {
		t.Fatal("expected IPv6 localhost dial target to be rejected")
	}
}

func TestFormProxyRejectsInvalidCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	form, _ := createFormProxyForTest(ctx, t, t.Name(), "invalid-captcha.example.com")

	body := url.Values{}
	body.Set("email", "test@example.com")
	body.Set(common.ParamPrivateCaptchaSolution, "invalid-captcha")

	resp := formProxySuite(t, form, body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestFormProxyRejectsWrongPropertyCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}
	form1, _, _, err := store.Impl().CreateNewForm(ctx, db_tests.CreateNewPropertyParams(user.ID, "wrong-property-one.example.com"), &dbgen.CreateFormParams{
		Name:              t.Name(),
		URL:               "https://example.com/submit",
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}
	property2, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "wrong-property-two.example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create second property: %v", err)
	}

	sitekey2 := db.UUIDToSiteKey(property2.ExternalID)
	puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey2, property2.Domain)
	if err != nil {
		t.Fatal(err)
	}

	body := url.Values{}
	body.Set("email", "test@example.com")
	body.Set(common.ParamPrivateCaptchaSolution, fmt.Sprintf("%s.%s", solutionsStr, puzzleStr))

	resp := formProxySuite(t, form1, body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Unexpected submit status code %d", resp.StatusCode)
	}
}

func TestFormProxySubmitsForm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	var mu sync.Mutex
	received := url.Values{}
	receivedHeaders := http.Header{}
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("downstream failed to parse form: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		defer mu.Unlock()
		received = r.PostForm
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}
	form, property, _, err := store.Impl().CreateNewForm(ctx, db_tests.CreateNewPropertyParams(user.ID, "submit-form.example.com"), &dbgen.CreateFormParams{
		Name:              t.Name(),
		URL:               downstream.URL,
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerSecond: 1,
		RequestsBurst:     5,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey, property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	body := url.Values{}
	body.Set("email", "test@example.com")
	body.Set("message", "hello")
	body.Set(common.ParamPrivateCaptchaSolution, fmt.Sprintf("%s.%s", solutionsStr, puzzleStr))

	resp := formProxySuite(t, form, body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Unexpected submit status code %d", resp.StatusCode)
	}

	for i := 0; i < 5; i++ {
		time.Sleep(formFlushInterval / 2)
		mu.Lock()
		called := received.Get("email") != ""
		mu.Unlock()
		if called {
			break
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if received.Get("email") == "" {
		t.Fatal("expected downstream endpoint to be called")
	}
	if received.Get("email") != "test@example.com" || received.Get("message") != "hello" {
		t.Fatalf("unexpected downstream form data: %v", received)
	}
	if received.Get(common.ParamPrivateCaptchaSolution) != "" {
		t.Fatalf("expected captcha solution field to be stripped")
	}
	if got := receivedHeaders.Get(common.HeaderContentType); got != common.ContentTypeURLEncoded {
		t.Fatalf("expected downstream content type %q, got %q", common.ContentTypeURLEncoded, got)
	}
}
