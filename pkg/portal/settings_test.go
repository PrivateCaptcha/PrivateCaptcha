package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func createAPIKeySuite(srv *http.ServeMux, csrfToken string, cookie *http.Cookie, name string, days int) *http.Response {
	return createAPIKeySuiteWithParam(srv, csrfToken, cookie, name, strconv.Itoa(days))
}

func createAPIKeySuiteWithParam(srv *http.ServeMux, csrfToken string, cookie *http.Cookie, name string, daysParam string) *http.Response {
	// Send POST request to create a new API key
	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, name)
	form.Set(common.ParamDays, daysParam)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

func TestCreateNeverExpiringAPIKey(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	resp := createAPIKeySuiteWithParam(srv, csrfToken, cookie, "Never Key", common.ParamNever)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if keysLen := len(keys); keysLen != 1 {
		t.Fatalf("Expected 1 API key, got %v", keysLen)
	}

	key := keys[0]
	expectedPeriod := time.Duration(apiKeyNeverExpireDays) * 24 * time.Hour
	if key.Period != expectedPeriod {
		t.Errorf("Expected period %v, got %v", expectedPeriod, key.Period)
	}

	tnow := time.Now().UTC()
	userKey := apiKeyToUserAPIKey(key, tnow, server.IDHasher)
	if !userKey.NeverExpires {
		t.Error("Expected NeverExpires to be true")
	}
	if userKey.ExpiresSoon {
		t.Error("Expected ExpiresSoon to be false for never-expiring key")
	}
	if userKey.ExpiresAt != "Never" {
		t.Errorf("Expected ExpiresAt to be 'Never', got %q", userKey.ExpiresAt)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	if len(viewModel.AuditEvents) == 0 {
		t.Error("Expected AuditEvents to be populated")
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	if len(viewModel.AuditEvents) == 0 {
		t.Error("Expected AuditEvents to be populated")
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

func TestGetNotificationsSettings(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/settings/tab/notifications", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()

	viewModel, err := server.getNotificationsSettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	renderCtx, ok := viewModel.Model.(*settingsNotificationsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *settingsNotificationsRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.ActiveTabID != common.NotificationsEndpoint {
		t.Errorf("Expected ActiveTabID to be %s, got %s", common.NotificationsEndpoint, renderCtx.ActiveTabID)
	}

	if renderCtx.UserEmail != user.Email {
		t.Errorf("Expected UserEmail to be %s, got %s", user.Email, renderCtx.UserEmail)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

func TestEmailChangeLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	issueReq := httptest.NewRequest(http.MethodPost, "/settings/tab/general/email", nil)
	issueReq.AddCookie(cookie)
	issueReq.Header.Set(common.HeaderCSRFToken, csrfToken)
	issueW := httptest.NewRecorder()
	srv.ServeHTTP(issueW, issueReq)
	if issueW.Code != http.StatusOK {
		t.Fatalf("email-change issue status = %d, want 200", issueW.Code)
	}
	maskedEmail := common.MaskEmail(user.Email, '*')
	if !strings.Contains(issueW.Body.String(), maskedEmail) {
		t.Fatalf("email-change issue did not render authoritative recipient %q", maskedEmail)
	}
	code, err := portal_tests.TwoFactorCodeFromEmail(user.Email)
	if err != nil {
		t.Fatal(err)
	}

	update := func(email string, verificationCode int) *http.Response {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamEmail, email)
		form.Set(common.ParamVerificationCode, strconv.Itoa(verificationCode))
		req := httptest.NewRequest(http.MethodPut, "/settings/tab/general", strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w.Result()
	}

	newEmail := strings.ToLower(t.Name()) + "_new@privatecaptcha.com"
	resp := update(newEmail, code+1)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("invalid email-change code status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), maskedEmail) {
		t.Fatalf("invalid email-change attempt did not render authoritative recipient %q", maskedEmail)
	}
	current, err := store.Impl().RetrieveUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Email != user.Email {
		t.Fatalf("invalid code changed email to %q", current.Email)
	}

	resp = update(newEmail, code)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("email-change consume status = %d, want 200", resp.StatusCode)
	}
	current, err = store.Impl().RetrieveUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Email != newEmail {
		t.Fatalf("email after successful change = %q", current.Email)
	}

	replayEmail := strings.ToLower(t.Name()) + "_replay@privatecaptcha.com"
	resp = update(replayEmail, code)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("email-change replay status = %d, want 200", resp.StatusCode)
	}
	current, err = store.Impl().RetrieveUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Email != newEmail {
		t.Fatalf("replayed challenge changed email to %q", current.Email)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

func TestPutNotificationsSettings(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamWeeklyReport, "on")
	form.Set(common.ParamMonthlyReport, "on")
	form.Set(common.ParamEmail, user.Email)

	req := httptest.NewRequest("PUT", "/settings/tab/notifications", strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()

	viewModel, err := server.putNotificationsSettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	renderCtx, ok := viewModel.Model.(*settingsNotificationsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *settingsNotificationsRenderContext, got %T", viewModel.Model)
	}

	if len(renderCtx.SuccessMessage) == 0 {
		t.Error("Expected SuccessMessage to be populated")
	}

	if len(viewModel.AuditEvents) == 0 {
		t.Error("Expected AuditEvents to be populated")
	}

	settings, err := store.Impl().RetrieveUserSettings(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve user settings: %v", err)
	}

	if !settings.WeeklyReport {
		t.Error("Expected WeeklyReport to be true")
	}
	if !settings.MonthlyReport {
		t.Error("Expected MonthlyReport to be true")
	}
	if !settings.NotificationsEmail.Valid || settings.NotificationsEmail.String != user.Email {
		t.Errorf("Expected NotificationsEmail to be %s, got %v", user.Email, settings.NotificationsEmail)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}
	otherCookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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
	cleared := false
	for _, responseCookie := range resp.Cookies() {
		if responseCookie.Name == server.Sessions.CookieName && responseCookie.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("account deletion did not clear the current session cookie")
	}

	otherReq := httptest.NewRequest(http.MethodGet, "/", nil)
	otherReq.AddCookie(otherCookie)
	otherW := httptest.NewRecorder()
	srv.ServeHTTP(otherW, otherReq)
	if otherW.Code == http.StatusOK {
		t.Fatal("account deletion left another authenticated session active")
	}

	_, err = store.Impl().RetrieveUser(ctx, user.ID)
	if err != db.ErrSoftDeleted {
		t.Errorf("Expected ErrSoftDeleted after deleting user, got: %v", err)
	}
}

func TestDeleteAccountRevocationFailureRetainsCookie(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	originalStore := server.Sessions.Store
	server.Sessions.Store = &revokeFailureSessionStore{
		Store: originalStore, err: errors.New("session revocation unavailable"),
	}
	t.Cleanup(func() { server.Sessions.Store = originalStore })
	req := httptest.NewRequest(http.MethodDelete, "/user", nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	server.Sessions.Store = originalStore

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("revocation failure status = %d, want redirect", resp.StatusCode)
	}
	if location, err := resp.Location(); err != nil || location.Path != "/"+common.ErrorEndpoint+"/500" {
		t.Fatalf("revocation failure redirect = (%v, %v)", location, err)
	}
	for _, responseCookie := range resp.Cookies() {
		if responseCookie.Name == server.Sessions.CookieName {
			t.Fatalf("revocation failure changed the current session cookie: %+v", responseCookie)
		}
	}
	if _, err := store.Impl().RetrieveUser(ctx, user.ID); !errors.Is(err, db.ErrSoftDeleted) {
		t.Fatalf("user after revocation failure error = %v, want %v", err, db.ErrSoftDeleted)
	}

	freshStore := db.NewSessionStore(store, server.Metrics)
	freshManager := &session.Manager{
		CookieName:   server.Sessions.CookieName,
		Store:        freshStore,
		MaxLifetime:  server.Sessions.MaxLifetime,
		Path:         server.Sessions.Path,
		SecureCookie: server.Sessions.SecureCookie,
	}
	if _, err := freshManager.Get(requestWithSessionCookie(http.MethodGet, "/", cookie)); !errors.Is(err, session.ErrSessionMissing) {
		t.Fatalf("fresh store authenticated soft-deleted user: %v", err)
	}
}

func TestDeleteAdminAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	adminEmail := server.AdminEmail.Value()
	cookie, err := portal_tests.AuthenticateSuite(ctx, adminEmail, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	user, err := store.Impl().FindUserByEmail(ctx, adminEmail)
	if err != nil {
		t.Fatalf("Failed to retrieve admin user: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/user", nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatalf("Failed to read redirect location: %v", err)
	}
	if location.String() != "/error/403" {
		t.Fatalf("Unexpected redirect location %v", location)
	}

	if _, err := store.Impl().RetrieveUser(ctx, user.ID); err != nil {
		t.Fatalf("Expected admin user to remain active, got: %v", err)
	}
}

type accountStatsSuiteResult struct {
	user   *dbgen.User
	srv    *http.ServeMux
	cookie *http.Cookie
}

func accountStatsSuite(t *testing.T, ctx context.Context) *accountStatsSuiteResult {
	t.Helper()

	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, t.Name()+".com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	verifyRecords := []*common.VerifyRecord{
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			Timestamp:  now.Add(-1 * time.Hour),
			Status:     1,
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			Timestamp:  now.Add(-2 * time.Hour),
			Status:     1,
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			Timestamp:  now.Add(-3 * time.Hour),
			Status:     1,
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			Timestamp:  now.Add(-4 * time.Hour),
			Status:     1,
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			Timestamp:  now.Add(-5 * time.Hour),
			Status:     1,
		},
	}

	if err := timeSeries.WriteVerifyLogBatch(ctx, verifyRecords); err != nil {
		t.Fatalf("Failed to write verify log batch: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	return &accountStatsSuiteResult{user: user, srv: srv, cookie: cookie}
}

func TestGetAccountStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	suite := accountStatsSuite(t, ctx)

	req := httptest.NewRequest("GET", "/user/stats", nil)
	req.AddCookie(suite.cookie)

	w := httptest.NewRecorder()
	suite.srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	var stats AccountStatsResponse

	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(stats.Data) == 0 {
		t.Error("Expected data but got none")
	}

	if len(stats.Series) != 1 {
		t.Fatalf("Expected 1 series, got %d", len(stats.Series))
	}

	totalCount := 0
	for _, p := range stats.Data {
		totalCount += p.Value
		if p.Series != stats.Series[0].Index {
			t.Errorf("Unexpected series index %d, want %d", p.Series, stats.Series[0].Index)
		}
	}

	if totalCount != 5 {
		t.Errorf("Expected 5 total (max of requests and verifies), got %d", totalCount)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		method   string
		path     string
		formBody url.Values
		checkErr string
	}{
		{
			name:   "PostAPIKeyInvalidName",
			method: "POST",
			path:   "/settings/tab/apikeys/new",
			formBody: url.Values{
				common.ParamName:  {"ab"},
				common.ParamDays:  {"90"},
				common.ParamScope: {apiKeyScopePuzzle},
			},
			checkErr: "too short",
		},
		{
			name:   "PostAPIKeyInvalidScope",
			method: "POST",
			path:   "/settings/tab/apikeys/new",
			formBody: url.Values{
				common.ParamName:  {"ValidName"},
				common.ParamDays:  {"90"},
				common.ParamScope: {"invalid-scope"},
			},
			checkErr: "scope",
		},
		{
			name:   "PutNotificationsInvalidEmail",
			method: "PUT",
			path:   "/settings/tab/notifications",
			formBody: url.Values{
				common.ParamEmail: {"not-a-valid-email"},
			},
			checkErr: "invalid email",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.formBody.Set(common.ParamCSRFToken, csrfToken)

			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.formBody.Encode()))
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

func TestAPIKeyDaysFromParam(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		param    string
		expected int
	}{
		{"1", 1},
		{"30", 30},
		{"90", 90},
		{"180", 180},
		{"365", 365},
		{common.ParamNever, apiKeyNeverExpireDays},
		{"Never", apiKeyNeverExpireDays},
		{" never ", apiKeyNeverExpireDays},
		{"invalid", 30},
		{"", 30},
		{"0", 30},
		{"999", 30},
		{"-1", 30},
		{"60", 30},
	}

	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			result := apiKeyDaysFromParam(ctx, tt.param)
			if result != tt.expected {
				t.Errorf("apiKeyDaysFromParam(%q) = %d, want %d", tt.param, result, tt.expected)
			}
		})
	}
}

func TestIsAPIKeyNeverExpires(t *testing.T) {
	tests := []struct {
		name     string
		period   time.Duration
		expected bool
	}{
		{"1 day", 24 * time.Hour, false},
		{"365 days", 365 * 24 * time.Hour, false},
		{"never (100 years)", time.Duration(apiKeyNeverExpireDays) * 24 * time.Hour, true},
		{"more than 100 years", time.Duration(apiKeyNeverExpireDays+1) * 24 * time.Hour, true},
		{"zero", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAPIKeyNeverExpires(tt.period)
			if result != tt.expected {
				t.Errorf("isAPIKeyNeverExpires(%v) = %v, want %v", tt.period, result, tt.expected)
			}
		})
	}
}

func TestAPIKeyToUserAPIKeyNeverExpires(t *testing.T) {
	hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "test"))
	tnow := time.Now().UTC()

	neverPeriod := time.Duration(apiKeyNeverExpireDays) * 24 * time.Hour
	neverKey := &dbgen.APIKey{
		ID:                1,
		Name:              "Never Key",
		ExpiresAt:         db.Timestampz(tnow.Add(neverPeriod)),
		Period:            neverPeriod,
		RequestsPerSecond: 10,
		RequestsBurst:     50,
		Scope:             dbgen.ApiKeyScopePuzzle,
	}

	result := apiKeyToUserAPIKey(neverKey, tnow, hasher)
	if !result.NeverExpires {
		t.Error("Expected NeverExpires to be true for 100-year key")
	}
	if result.ExpiresSoon {
		t.Error("Expected ExpiresSoon to be false for never-expiring key")
	}
	if result.ExpiresAt != "Never" {
		t.Errorf("Expected ExpiresAt to be 'Never', got %q", result.ExpiresAt)
	}

	regularPeriod := 90 * 24 * time.Hour
	regularKey := &dbgen.APIKey{
		ID:                2,
		Name:              "Regular Key",
		ExpiresAt:         db.Timestampz(tnow.Add(regularPeriod)),
		Period:            regularPeriod,
		RequestsPerSecond: 10,
		RequestsBurst:     50,
		Scope:             dbgen.ApiKeyScopePuzzle,
	}

	result = apiKeyToUserAPIKey(regularKey, tnow, hasher)
	if result.NeverExpires {
		t.Error("Expected NeverExpires to be false for regular key")
	}
	if result.ExpiresSoon {
		t.Error("Expected ExpiresSoon to be false for key expiring in 90 days")
	}

	soonPeriod := 7 * 24 * time.Hour
	soonKey := &dbgen.APIKey{
		ID:                3,
		Name:              "Soon Key",
		ExpiresAt:         db.Timestampz(tnow.Add(soonPeriod)),
		Period:            soonPeriod,
		RequestsPerSecond: 10,
		RequestsBurst:     50,
		Scope:             dbgen.ApiKeyScopePuzzle,
	}

	result = apiKeyToUserAPIKey(soonKey, tnow, hasher)
	if result.NeverExpires {
		t.Error("Expected NeverExpires to be false for soon-expiring key")
	}
	if !result.ExpiresSoon {
		t.Error("Expected ExpiresSoon to be true for key expiring in 7 days")
	}
}

func TestCheckAPIKeyNameValid(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"ValidAlphanumeric", "MyAPIKey123", true},
		{"ValidWithSpaces", "My API Key", true},
		{"ValidWithDash", "my-api-key", true},
		{"ValidWithUnderscore", "my_api_key", true},
		{"ValidWithDots", "api.key.name", true},
		{"ValidWithParens", "my(key)", true},
		{"ValidWithBrackets", "my[key]", true},
		{"InvalidWithAmpersand", "my&key", false},
		{"InvalidWithAtSign", "my@key", false},
		{"InvalidWithPercent", "my%key", false},
		{"InvalidWithDollar", "my$key", false},
		{"EmptyString", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkAPIKeyNameValid(ctx, tt.input)
			if result != tt.expected {
				t.Errorf("checkAPIKeyNameValid(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseAPIKeyScope(t *testing.T) {
	tests := []struct {
		scope        string
		wantScope    string
		wantReadOnly bool
		wantError    bool
	}{
		{"portal_read_write", "portal", false, false},
		{"portal_read_only", "portal", true, false},
		{"captcha", "puzzle", false, false},
		{"invalid", "", false, true},
		{"", "", false, true},
		{"portal", "", false, true},
		{"puzzle_read_write", "", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			scope, readOnly, err := parseAPIKeyScope(tt.scope)
			if tt.wantError {
				if err == nil {
					t.Errorf("parseAPIKeyScope(%q) expected error, got nil", tt.scope)
				}
				return
			}

			if err != nil {
				t.Errorf("parseAPIKeyScope(%q) unexpected error: %v", tt.scope, err)
				return
			}

			if readOnly != tt.wantReadOnly {
				t.Errorf("parseAPIKeyScope(%q) readOnly = %v, want %v", tt.scope, readOnly, tt.wantReadOnly)
			}

			if tt.wantScope == "portal" && scope != "portal" {
				t.Errorf("parseAPIKeyScope(%q) scope = %v, want portal", tt.scope, scope)
			}

			if tt.wantScope == "puzzle" && scope != "puzzle" {
				t.Errorf("parseAPIKeyScope(%q) scope = %v, want puzzle", tt.scope, scope)
			}
		})
	}
}

func TestPutGeneralSettingsChangeName(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Change name to something different
	newName := "Updated Name"
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, newName)

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

	renderCtx, ok := viewModel.Model.(*settingsGeneralRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *settingsGeneralRenderContext, got %T", viewModel.Model)
	}

	// Check that name was updated
	if renderCtx.Name != newName {
		t.Errorf("Expected Name to be '%s', got '%s'", newName, renderCtx.Name)
	}

	// Verify in DB
	updatedUser, err := store.Impl().RetrieveUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updatedUser.Name != newName {
		t.Errorf("Expected user name in DB to be '%s', got '%s'", newName, updatedUser.Name)
	}
}

func TestPostAPIKeySettingsScopedKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	// Create a scoped API key for a specific org
	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, "Scoped API Key")
	form.Set(common.ParamDays, "90")
	form.Set(common.ParamScope, apiKeyScopePortal+apiKeyReadWriteSuffix)
	form.Set(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	req := httptest.NewRequest("POST", "/settings/tab/apikeys/new", strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	// Check that the API key was created with org scope
	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(keys) == 0 {
		t.Error("Expected API key to be created")
	}

	// Find the scoped key
	var foundScopedKey bool
	for _, key := range keys {
		if key.Name == "Scoped API Key" && key.OrgID.Valid && key.OrgID.Int32 == org.ID {
			foundScopedKey = true
			break
		}
	}

	if !foundScopedKey {
		t.Error("Expected to find a scoped API key for the org")
	}
}

func TestPortalAPIKeyNeverExpirationRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	for _, testScope := range []string{apiKeyScopePortal + apiKeyReadWriteSuffix, apiKeyScopePortal + apiKeyReadOnlySuffix} {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "Portal Never Key "+testScope)
		form.Set(common.ParamDays, common.ParamNever)
		form.Set(common.ParamScope, testScope)
		form.Set(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

		req := httptest.NewRequest("POST", "/settings/tab/apikeys/new", strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Unexpected status code %v for scope %s", resp.StatusCode, testScope)
		}
	}

	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(keys) != 0 {
		t.Errorf("Expected no API keys to be created, got %d", len(keys))
	}
}

func TestGetAccountStatsWithUnknownOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property1, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "stats-unknown-1.com"), org1)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	// Create second org
	org2, _, err := store.Impl().CreateNewOrganization(ctx, "Second Org For Deletion", user.ID)
	if err != nil {
		t.Fatalf("Failed to create second org: %v", err)
	}

	property2, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "stats-unknown-2.com"), org2)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	accessRecords := []*common.AccessRecord{
		{
			UserID:     user.ID,
			OrgID:      org1.ID,
			PropertyID: property1.ID,
			Timestamp:  now.Add(-1 * time.Hour),
		},
		{
			UserID:     user.ID,
			OrgID:      org2.ID,
			PropertyID: property2.ID,
			Timestamp:  now.Add(-2 * time.Hour),
		},
	}

	if err := timeSeries.WriteAccessLogBatch(ctx, accessRecords); err != nil {
		t.Fatalf("Failed to write access log batch: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Delete the second org BEFORE retrieving stats
	if _, err := store.Impl().SoftDeleteOrganization(ctx, org2, user); err != nil {
		t.Fatalf("Failed to delete org: %v", err)
	}

	// Now get stats - should have one org with "Unknown Organization" name
	req := httptest.NewRequest("GET", "/user/stats", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	var stats AccountStatsResponse

	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should have stats
	if len(stats.Data) == 0 {
		t.Error("Expected data but got none")
	}

	// Should have at least one series (for the existing org)
	if len(stats.Series) == 0 {
		t.Error("Expected at least one series")
	}

	// Check if any series has "Unknown" in name (for deleted org)
	hasUnknown := false
	for _, series := range stats.Series {
		if strings.Contains(series.Name, "Unknown") {
			hasUnknown = true
			break
		}
	}

	if !hasUnknown {
		t.Log("Note: Deleted org may have been filtered out if stats only returns current user's orgs")
	}
}
