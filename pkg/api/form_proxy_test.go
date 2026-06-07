package api

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/jackc/pgx/v5/pgtype"
)

type stubSubmitFormURLVerifier struct {
	err  error
	urls []string
}

type submitFormRoundTripFunc func(*http.Request) (*http.Response, error)

func (f submitFormRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type trackingReadCloser struct {
	reads  int
	closed bool
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
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

	form, property, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, domain),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
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
	server := &Server{
		BusinessDB:        store,
		FormURLVerifier:   verifier,
		FormSubmitLogChan: make(chan *common.FormSubmitRecord, 1),
		FailingForms:      common.NewExpiringCounterMap[int32](),
	}
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
	server := &Server{
		BusinessDB:        store,
		FormURLVerifier:   &stubSubmitFormURLVerifier{},
		FormSubmitLogChan: make(chan *common.FormSubmitRecord, 1),
		FailingForms:      common.NewExpiringCounterMap[int32](),
	}
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
	server := &Server{
		BusinessDB:        store,
		FormURLVerifier:   &stubSubmitFormURLVerifier{},
		FormSubmitLogChan: make(chan *common.FormSubmitRecord, 2),
		FailingForms:      common.NewExpiringCounterMap[int32](),
	}
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

func TestProcessFormSubmissionClonesClientWhenRedirectsEnabled(t *testing.T) {
	redirectHits := 0
	successHits := 0
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			redirectHits++
			http.Redirect(w, r, "/success", http.StatusFound)
		case "/success":
			successHits++
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer downstream.Close()

	verifier := &stubSubmitFormURLVerifier{}
	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL + "/redirect", Method: dbgen.FormMethodPost, RetryRequestCount: 0, Enabled: true, Active: true, RedirectCount: 1}
	server := &Server{
		FormsClient:       common.NewFormHTTPClient(),
		FormURLVerifier:   verifier,
		FormSubmitLogChan: make(chan *common.FormSubmitRecord, 1),
		FailingForms:      common.NewExpiringCounterMap[int32](),
	}
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	if err := server.processFormSubmission(context.Background(), form, submission); err != nil {
		t.Fatalf("expected redirect-enabled submission to succeed, got %v", err)
	}
	if redirectHits != 1 {
		t.Fatalf("expected one redirect response, got %d", redirectHits)
	}
	if successHits != 1 {
		t.Fatalf("expected redirect target to be hit once, got %d", successHits)
	}
}

func TestFormProxyCachedFormUpdatesRateLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}
	form, property, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "cached-form.example.com"),
		&dbgen.CreateFormParams{
			Name:              t.Name(),
			URL:               "https://example.com/submit",
			Fields:            []byte(`{}`),
			Enabled:           true,
			RequestsPerMinute: 24,
			RetryRequestCount: 0,
			Method:            dbgen.FormMethodPost,
		},
		org,
	)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	sitekey := db.UUIDToSiteKey(property.ExternalID)
	puzzleStr, solutionsStr, err := solutionsSuite(ctx, sitekey, property.Domain)
	if err != nil {
		t.Fatal(err)
	}

	oldRateLimiter := server.RateLimiter
	recordingRateLimiter := &ratelimit.StubRateLimiter{Header: cfg.Get(common.RateLimitHeaderKey).Value()}
	server.RateLimiter = recordingRateLimiter
	t.Cleanup(func() {
		server.RateLimiter = oldRateLimiter
	})

	srv := http.NewServeMux()
	server.Setup("", true /*verbose*/, common.NoopMiddleware).Register(srv)

	body := url.Values{}
	body.Set("email", "test@example.com")
	body.Set(common.ParamPrivateCaptchaSolution, fmt.Sprintf("%s.%s", solutionsStr, puzzleStr))

	req := httptest.NewRequest(http.MethodPost, "/"+common.FormEndpoint+"/"+db.UUIDToString(form.ExternalID), strings.NewReader(body.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set(cfg.Get(common.RateLimitHeaderKey).Value(), common_test.GenerateRandomIPv4())

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("Unexpected submit status code %d", w.Code)
	}
	if recordingRateLimiter.UpdateCalls != 1 {
		t.Fatalf("expected one rate limit update, got %d", recordingRateLimiter.UpdateCalls)
	}
	if recordingRateLimiter.UpdatedCapacity != 34 {
		t.Fatalf("expected updated capacity 34, got %d", recordingRateLimiter.UpdatedCapacity)
	}
	expectedLeakInterval := time.Minute / 24
	if recordingRateLimiter.UpdatedLeakInterval != expectedLeakInterval {
		t.Fatalf("expected leak interval %s, got %s", expectedLeakInterval, recordingRateLimiter.UpdatedLeakInterval)
	}
}

func TestSubmitFormWithRetryReturnsSuccessResult(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 0, Enabled: true, Active: true}
	client := common.NewFormHTTPClient()
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitFormWithRetry(context.Background(), client, form, submission)
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

func TestSubmitFormWithRetryTreatsRedirectAsFailureWhenDisabled(t *testing.T) {
	redirectHits := 0
	successHits := 0
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			redirectHits++
			http.Redirect(w, r, "/success", http.StatusFound)
		case "/success":
			successHits++
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL + "/redirect", Method: dbgen.FormMethodPost, RetryRequestCount: 1, Enabled: true, Active: true, RedirectCount: 0}
	client := common.NewFormHTTPClient()
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitFormWithRetry(context.Background(), client, form, submission)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Success {
		t.Fatal("expected redirect response to be treated as failure")
	}
	if result.StatusCode != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, result.StatusCode)
	}
	if redirectHits != 1 {
		t.Fatalf("expected one redirect attempt, got %d", redirectHits)
	}
	if successHits != 0 {
		t.Fatalf("expected redirect target not to be hit, got %d", successHits)
	}
}

func TestSubmitFormWithRetryFollowsRedirectWhenEnabled(t *testing.T) {
	redirectHits := 0
	successHits := 0
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			redirectHits++
			http.Redirect(w, r, "/success", http.StatusFound)
		case "/success":
			successHits++
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL + "/redirect", Method: dbgen.FormMethodPost, RetryRequestCount: 1, Enabled: true, Active: true, RedirectCount: 1}
	verifier := &stubSubmitFormURLVerifier{}
	client := common.NewFormRedirectHTTPClient(verifier, form.RedirectCount)
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitFormWithRetry(context.Background(), client, form, submission)
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.Success {
		t.Fatal("expected redirect to be followed")
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	if redirectHits != 1 {
		t.Fatalf("expected one redirect response, got %d", redirectHits)
	}
	if successHits != 1 {
		t.Fatalf("expected redirect target to be hit once, got %d", successHits)
	}
}

func TestSubmitFormWithRetryReturnsFailureResult(t *testing.T) {
	var attempts int
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 0, Enabled: true, Active: true}
	client := common.NewFormHTTPClient()
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitFormWithRetry(context.Background(), client, form, submission)
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

func TestSubmitFormWithRetryReturnsFailureResultAfterRetries(t *testing.T) {
	var attempts int
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 2, Enabled: true, Active: true}
	client := common.NewFormHTTPClient()
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitFormWithRetry(context.Background(), client, form, submission)
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

func TestSubmitFormWithRetryRetriesSelectedHttpStatuses(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
	}{
		{name: "InternalServerError", statusCode: http.StatusInternalServerError},
		{name: "TooManyRequests", statusCode: http.StatusTooManyRequests},
		{name: "RequestTimeout", statusCode: http.StatusRequestTimeout},
		{name: "TooEarly", statusCode: http.StatusTooEarly},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int
			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				w.WriteHeader(tc.statusCode)
			}))
			defer downstream.Close()

			form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 1, Enabled: true, Active: true}
			client := common.NewFormHTTPClient()
			submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

			result := SubmitFormWithRetry(context.Background(), client, form, submission)
			if result == nil {
				t.Fatal("expected result")
			}
			if result.StatusCode != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, result.StatusCode)
			}
			if attempts != 2 {
				t.Fatalf("expected 2 attempts, got %d", attempts)
			}
		})
	}
}

func TestSubmitFormWithRetryRetriesTransportError(t *testing.T) {
	var attempts int
	expectedErr := errors.New("transport error")
	client := &http.Client{Transport: submitFormRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return nil, expectedErr
	})}
	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: "https://example.com/submit", Method: dbgen.FormMethodPost, RetryRequestCount: 1, Enabled: true, Active: true}
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitFormWithRetry(context.Background(), client, form, submission)
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.Valid {
		t.Fatal("expected valid result")
	}
	if result.StatusCode != 0 {
		t.Fatalf("expected empty status, got %d", result.StatusCode)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestSubmitFormWithRetryDoesNotRetryBadRequest(t *testing.T) {
	var attempts int
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid request body that should only be logged once"))
	}))
	defer downstream.Close()

	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: downstream.URL, Method: dbgen.FormMethodPost, RetryRequestCount: 2, Enabled: true, Active: true}
	client := common.NewFormHTTPClient()
	submission := &FormSubmission{FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result := SubmitFormWithRetry(context.Background(), client, form, submission)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Success {
		t.Fatal("expected failure result")
	}
	if result.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, result.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestSubmitFormOnceDoesNotReadSuccessfulResponseBody(t *testing.T) {
	body := &trackingReadCloser{}
	client := &http.Client{Transport: submitFormRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header:     make(http.Header),
		}, nil
	})}
	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: "https://example.com/submit", Method: dbgen.FormMethodPost, RetryRequestCount: 0, Enabled: true, Active: true}
	submission := &FormSubmission{ID: "sub-1", FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result, err := submitFormOnce(context.Background(), client, form, submission)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil || !result.Success {
		t.Fatal("expected success result")
	}
	if body.reads != 0 {
		t.Fatalf("expected response body not to be read, got %d reads", body.reads)
	}
	if !body.closed {
		t.Fatal("expected response body to be closed")
	}
}

func TestSubmitFormOnceDoesNotReadBadRequestBody(t *testing.T) {
	body := &trackingReadCloser{}
	client := &http.Client{Transport: submitFormRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       body,
			Header:     make(http.Header),
		}, nil
	})}
	form := &dbgen.Form{ID: 123, PropertyID: 456, OrgOwnerID: db.Int(7), OrgID: db.Int(8), ExternalID: db.TestPropertyUUID, URL: "https://example.com/submit", Method: dbgen.FormMethodPost, RetryRequestCount: 0, Enabled: true, Active: true}
	submission := &FormSubmission{ID: "sub-1", FormExternalID: db.UUIDToString(form.ExternalID), Values: url.Values{"email": {"test@example.com"}}}

	result, err := submitFormOnce(context.Background(), client, form, submission)
	if !errors.Is(err, errFormSubmitFailed) {
		t.Fatalf("expected form submit failure, got %v", err)
	}
	if result == nil || result.Success {
		t.Fatal("expected failure result")
	}
	if body.reads != 0 {
		t.Fatalf("expected response body not to be read, got %d reads", body.reads)
	}
	if !body.closed {
		t.Fatal("expected response body to be closed")
	}
}

func TestFormHTTPClientDoesNotFollowRedirectsByDefault(t *testing.T) {
	client := common.NewFormHTTPClient()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/form", nil)

	err := client.CheckRedirect(req, nil)
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("expected redirect response to be returned, got %v", err)
	}
}

func TestFormRedirectClientRejectsUnsafeRedirect(t *testing.T) {
	expectedErr := errors.New("unsafe redirect URL")
	verifier := &stubSubmitFormURLVerifier{err: expectedErr}
	client := common.NewFormRedirectHTTPClient(verifier, 1)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/form", nil)

	err := client.CheckRedirect(req, nil)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected redirect verifier error, got %v", err)
	}
	if len(verifier.urls) != 1 || verifier.urls[0] != "http://127.0.0.1/form" {
		t.Fatalf("expected redirect URL to be verified, got %v", verifier.urls)
	}
}

func TestFormRedirectClientCapsRedirectCount(t *testing.T) {
	verifier := &stubSubmitFormURLVerifier{}
	client := common.NewFormRedirectHTTPClient(verifier, 99)
	req := httptest.NewRequest(http.MethodGet, "https://example.com/form", nil)
	via := make([]*http.Request, 0, 11)
	for range 10 {
		via = append(via, req)
	}

	if err := client.CheckRedirect(req, via); err != nil {
		t.Fatalf("expected capped redirect count to allow 10 redirects, got %v", err)
	}
	via = append(via, req)
	if err := client.CheckRedirect(req, via); err == nil {
		t.Fatal("expected redirect client to stop after capped redirect count")
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
	form1, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "wrong-property-one.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
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
	form, property, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "submit-form.example.com"),
		db_tests.CreateNewFormParams(user.ID, downstream.URL),
		org)
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
