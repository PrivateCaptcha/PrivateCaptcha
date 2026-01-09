package portal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func createAPIKeySuite(srv *http.ServeMux, csrfToken string, cookie *http.Cookie, name string, days int) *http.Response {
	// Send POST request to create a new API key
	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, name)
	form.Set(common.ParamDays, strconv.Itoa(days))
	form.Set(common.ParamScope, apiKeyScopePuzzle)

	req := httptest.NewRequest("POST", "/settings/tab/apikeys/new", strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	return w.Result()
}

func TestCreateAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	name := "My API Key"
	resp := createAPIKeySuite(srv, csrfToken, cookie, name, 90)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if keysLen := len(keys); keysLen != 1 {
		t.Errorf("Unexpected number of API keys: %v", keysLen)
	}

	_ = createAPIKeySuite(srv, csrfToken, cookie, name, 180)
	keys, err = store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keysLen := len(keys); keysLen != 1 {
		t.Errorf("Duplicate key was created. Keys count: %v", keysLen)
	}
}

func TestDeleteAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	key, _, err := store.Impl().CreateAPIKey(ctx, user, tests.CreateNewPuzzleAPIKeyParams("My API Key", time.Now(), 24*time.Hour, 10.0))
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/apikeys/%v", server.IDHasher.Encrypt(int(key.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keysLen := len(keys); keysLen != 0 {
		t.Errorf("API key was not deleted. Keys count: %v", keysLen)
	}
}

func TestRotateAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	tnow := time.Now().UTC()
	key, _, err := store.Impl().CreateAPIKey(ctx, user, tests.CreateNewPuzzleAPIKeyParams("My API Key", tnow.Add(-24*time.Hour), 23*time.Hour, 10.0))
	if err != nil {
		t.Fatal(err)
	}
	secretOld := db.UUIDToSecret(key.ExternalID)

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	req := httptest.NewRequest("POST", fmt.Sprintf("/apikeys/%v", server.IDHasher.Encrypt(int(key.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keysLen := len(keys); keysLen != 1 {
		t.Errorf("Unexpected number of API keys: %v", keysLen)
	}
	if !keys[0].ExpiresAt.Valid || !keys[0].ExpiresAt.Time.After(tnow.Add(22*time.Hour)) {
		t.Errorf("Key expiration was not rotated")
	}

	if secret := db.UUIDToSecret(keys[0].ExternalID); secret == secretOld {
		t.Error("Key external ID was not rotated")
	}
}

func TestGetSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/settings", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	viewModel, err := server.getSettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if !strings.HasSuffix(viewModel.View, "page.html") {
		t.Errorf("Expected view to end with page.html, got %s", viewModel.View)
	}
}

func TestGetGeneralSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/settings/tab/general", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	viewModel, err := server.getGeneralSettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	renderCtx, ok := viewModel.Model.(*settingsGeneralRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *settingsGeneralRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.Name != user.Name {
		t.Errorf("Expected Name to be %s, got %s", user.Name, renderCtx.Name)
	}

	if viewModel.AuditEvent == nil {
		t.Error("Expected AuditEvent to be populated")
	}
}

func TestGetAPIKeysSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/settings/tab/apikeys", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	viewModel, err := server.getAPIKeysSettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	renderCtx, ok := viewModel.Model.(*settingsAPIKeysRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *settingsAPIKeysRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.Keys == nil {
		t.Error("Expected Keys to be initialized (even if empty)")
	}

	if viewModel.AuditEvent == nil {
		t.Error("Expected AuditEvent to be populated")
	}
}

func TestGetUsageSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/settings/tab/usage", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	viewModel, err := server.getUsageSettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	renderCtx, ok := viewModel.Model.(*settingsUsageRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *settingsUsageRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.OrgsCount == 0 {
		t.Error("Expected OrgsCount to be at least 1")
	}
}

func TestGetSettingsTab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/settings/tab/general", nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamTab, "general")

	w := httptest.NewRecorder()

	viewModel, err := server.getSettingsTab(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if !strings.HasSuffix(viewModel.View, "tab.html") {
		t.Errorf("Expected view to end with tab.html, got %s", viewModel.View)
	}
}

func TestEditEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/settings/tab/general/email", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	viewModel, err := server.editEmail(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	renderCtx, ok := viewModel.Model.(*settingsGeneralRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *settingsGeneralRenderContext, got %T", viewModel.Model)
	}

	if !renderCtx.EditEmail {
		t.Error("Expected EditEmail to be true")
	}

	if len(renderCtx.TwoFactorEmail) == 0 {
		t.Error("Expected TwoFactorEmail to be populated")
	}
}

func TestPutGeneralSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, user.Name)

	req := httptest.NewRequest("PUT", "/settings/tab/general", strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()

	viewModel, err := server.putGeneralSettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}
}

func TestDeleteAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", "/user", nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	_, err = store.Impl().RetrieveUser(ctx, user.ID)
	if err != db.ErrSoftDeleted {
		t.Errorf("Expected ErrSoftDeleted after deleting user, got: %v", err)
	}
}

func TestGetAccountStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "stats-example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	accessRecords := []*common.AccessRecord{
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			Timestamp:  now.Add(-1 * time.Hour),
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			Timestamp:  now.Add(-2 * time.Hour),
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			Timestamp:  now.Add(-3 * time.Hour),
		},
	}

	if err := timeSeries.WriteAccessLogBatch(ctx, accessRecords); err != nil {
		t.Fatalf("Failed to write access log batch: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	req := httptest.NewRequest("GET", "/user/stats", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	type point struct {
		Date  int64 `json:"x"`
		Value int   `json:"y"`
	}

	var stats struct {
		Data []*point `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(stats.Data) == 0 {
		t.Error("Expected data but got none")
	}

	totalCount := 0
	for _, p := range stats.Data {
		totalCount += p.Value
	}

	if totalCount != 3 {
		t.Errorf("Expected 3 total requests, got %d", totalCount)
	}
}

func TestAPIKeyEndpointsInvalidPathArg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
	}{
		{"RotateAPIKeyInvalidID", "POST", "/apikeys/invalid-id", http.StatusSeeOther},
		{"DeleteAPIKeyInvalidID", "DELETE", "/apikeys/invalid-id", http.StatusSeeOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderCSRFToken, csrfToken)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestAPIKeyEndpointsWrongOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	owner, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	key, _, err := store.Impl().CreateAPIKey(ctx, owner, tests.CreateNewPuzzleAPIKeyParams("OwnerKey", time.Now(), 24*time.Hour, 10.0))
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_intruder", testPlan)
	if err != nil {
		t.Fatalf("Failed to create intruder account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	keyID := server.IDHasher.Encrypt(int(key.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user2.ID)))

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
	}{
		{"RotateAPIKeyWrongOwner", "POST", fmt.Sprintf("/apikeys/%s", keyID), http.StatusSeeOther},
		{"DeleteAPIKeyWrongOwner", "DELETE", fmt.Sprintf("/apikeys/%s", keyID), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderCSRFToken, csrfToken)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestSettingsEndpointsInvalidFormArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		path     string
		formBody url.Values
		checkErr string
	}{
		{
			name: "PostAPIKeyInvalidName",
			path: "/settings/tab/apikeys/new",
			formBody: url.Values{
				common.ParamName:  {"ab"},
				common.ParamDays:  {"90"},
				common.ParamScope: {apiKeyScopePuzzle},
			},
			checkErr: "too short",
		},
		{
			name: "PostAPIKeyInvalidScope",
			path: "/settings/tab/apikeys/new",
			formBody: url.Values{
				common.ParamName:  {"ValidName"},
				common.ParamDays:  {"90"},
				common.ParamScope: {"invalid-scope"},
			},
			checkErr: "scope",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.formBody.Set(common.ParamCSRFToken, csrfToken)

			req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.formBody.Encode()))
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			body := w.Body.String()
			if !strings.Contains(strings.ToLower(body), tc.checkErr) {
				t.Errorf("%s: expected response to contain '%s'", tc.name, tc.checkErr)
			}
		})
	}
}

func TestSettingsTabInvalidTab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/settings/tab/nonexistent-tab", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK (fallback to default tab), got %d", w.Code)
	}
}
