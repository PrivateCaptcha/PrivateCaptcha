package portal

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PuerkitoBio/goquery"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestWebhookPrefixFromURL(t *testing.T) {
	testCases := []struct {
		name string
		url  string
		want string
	}{
		{name: "FirstPathSegment", url: "https://hooks.example.com/submit/form", want: "hooks.example.com/submit"},
		{name: "LongSegmentTrimmed", url: "https://hooks.example.com/abcdefghijklmnop/rest", want: "hooks.example.com/abcdefghijkl"},
		{name: "NoPath", url: "https://hooks.example.com", want: "hooks.example.com"},
		{name: "InvalidURLFallsBack", url: "not a valid url", want: "not a valid url"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webhookPrefixFromURL(tc.url); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestParseRequestsPerMinuteReturnsIntegerValue(t *testing.T) {
	rpm, err := parseRequestsPerMinute(t.Context(), "24")
	if err != nil {
		t.Fatalf("expected parse to succeed, got %v", err)
	}
	if rpm != 24 {
		t.Fatalf("expected requests per minute 24, got %d", rpm)
	}
}

func TestFormToUserForm(t *testing.T) {
	hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "test-salt"))
	form := &dbgen.Form{
		ID:      34,
		OrgID:   pgtype.Int4{Int32: 56, Valid: true},
		URL:     "https://hooks.example.com/submit/form",
		Method:  dbgen.FormMethodPut,
		Enabled: true,
	}

	userForm := formToUserForm(form, hasher)
	if userForm == nil {
		t.Fatal("expected user form")
	}
	if userForm.ID != hasher.Encrypt(int(form.ID)) {
		t.Fatalf("expected encrypted form ID %q, got %q", hasher.Encrypt(int(form.ID)), userForm.ID)
	}
	if userForm.OrgID != hasher.Encrypt(int(form.OrgID.Int32)) {
		t.Fatalf("expected encrypted org ID %q, got %q", hasher.Encrypt(int(form.OrgID.Int32)), userForm.OrgID)
	}
	if userForm.Name != form.Name {
		t.Fatalf("expected property name %q, got %q", form.Name, userForm.Name)
	}
	if userForm.WebhookPrefix != "hooks.example.com/submit" {
		t.Fatalf("expected webhook prefix %q, got %q", "hooks.example.com/submit", userForm.WebhookPrefix)
	}
	if userForm.Method != http.MethodPut {
		t.Fatalf("expected method %q, got %q", http.MethodPut, userForm.Method)
	}
	if !userForm.Enabled {
		t.Fatal("expected enabled form")
	}
}

func TestRenderFormsPaginationControls(t *testing.T) {
	platformCtx := &PlatformRenderContext{
		GitCommit:      "qwerty123",
		Enterprise:     true,
		licenseService: server.LicenseService,
	}

	buf, err := server.RenderResponse(t.Context(), "portal/forms.html", &orgFormsRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{CurrentOrg: stubOrg("123")},
		PaginationRenderContext: PaginationRenderContext{From: 1, To: 30, Count: 31, Page: 0, PerPage: 30},
		Forms:                   []*userForm{stubForm("Newsletter Signup", "123")},
	}, &RequestContext{Path: server.RelURL("/org/123/forms")}, platformCtx)
	if err != nil {
		t.Fatal(err)
	}

	document := portal_tests.ParseHTML(t, buf)
	buttons := document.Find("button[hx-target=\"#forms\"]")
	if buttons.Length() != 2 {
		t.Fatalf("expected 2 pagination buttons, got %d", buttons.Length())
	}
	buttons.Each(func(i int, s *goquery.Selection) {
		if s.AttrOr("hx-get", "") != "/org/123/forms" {
			t.Fatalf("expected pagination button to use forms endpoint, got %q", s.AttrOr("hx-get", ""))
		}
	})
}

func TestGetNewOrgForm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/org/%s/form/new", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Website integration") {
		t.Fatal("expected website integration step")
	}
	if !strings.Contains(body, `name="url"`) {
		t.Fatal("expected URL input field")
	}
	if !strings.Contains(body, fmt.Sprintf("/org/%s?tab=%s", server.IDHasher.Encrypt(int(org.ID)), common.FormsEndpoint)) {
		t.Fatal("expected cancel link back to forms area")
	}
	if strings.Contains(body, "Server integration") {
		t.Fatal("did not expect server integration step")
	}
}

func TestPostNewOrgForm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	formName := t.Name() + " Contact"
	formData := url.Values{}
	formData.Set(common.ParamCSRFToken, server.XSRF.Token(fmt.Sprintf("%d", user.ID)))
	formData.Set(common.ParamName, formName)
	formData.Set(common.ParamDomain, "example.com")
	formData.Set(common.ParamURL, "https://hooks.example.com/submit")
	formData.Set(common.ParamIgnoreError, "true")

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/new", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(formData.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Unexpected status code %d", w.Code)
	}

	forms, _, err := store.Impl().RetrieveOrgForms(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 {
		t.Fatalf("expected 1 form, got %d", len(forms))
	}

	createdForm := forms[0]
	if createdForm.Name != formName {
		t.Fatalf("expected form name %q, got %q", formName, createdForm.Name)
	}
	if createdForm.URL != "https://hooks.example.com/submit" {
		t.Fatalf("expected form URL to be preserved, got %q", createdForm.URL)
	}
	if !createdForm.Enabled {
		t.Fatal("expected created form to be enabled")
	}
	if createdForm.Method != dbgen.FormMethodPost {
		t.Fatalf("expected form method %q, got %q", dbgen.FormMethodPost, createdForm.Method)
	}
	if createdForm.RequestsPerMinute != 10 {
		t.Fatalf("expected requests per minute 10, got %d", createdForm.RequestsPerMinute)
	}
	if createdForm.RetryRequestCount != 0 {
		t.Fatalf("expected retry count 0, got %d", createdForm.RetryRequestCount)
	}

	createdProperty, err := store.Impl().GetCachedPropertyByID(ctx, createdForm.PropertyID)
	if err != nil {
		t.Fatalf("expected backing property, got %v", err)
	}
	if createdProperty == nil {
		t.Fatal("expected backing property to exist")
	}
	if createdProperty.Domain != "example.com" {
		t.Fatalf("expected backing property domain %q, got %q", "example.com", createdProperty.Domain)
	}

	formGUID := db.UUIDToString(createdForm.ExternalID)
	expectedFormURL := server.APIURL + "/form/" + formGUID
	if !strings.Contains(w.Body.String(), expectedFormURL) {
		t.Fatal("expected integration step to include public form endpoint")
	}
}

func TestGetFormDashboard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "dashboard.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	formID := server.IDHasher.Encrypt(int(form.ID))
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/org/%s/form/%s", orgID, formID), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Unexpected status code %d", w.Code)
	}

	body := w.Body.String()
	for _, label := range []string{"Reports", "Integrations", "Settings", "Audit logs"} {
		if !strings.Contains(body, label) {
			t.Fatalf("expected %q tab in dashboard", label)
		}
	}
	if strings.Contains(body, "Rules") {
		t.Fatal("did not expect rules tab in form dashboard")
	}
	if !strings.Contains(body, "Form Requests") {
		t.Fatal("expected reports content in form dashboard")
	}
}

func TestGetFormDashboardIntegrationsTab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, property, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "integrations.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	formID := server.IDHasher.Encrypt(int(form.ID))
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/org/%s/form/%s?tab=%s", orgID, formID, common.IntegrationsEndpoint), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Unexpected status code %d", w.Code)
	}

	body := w.Body.String()
	expectedFormURL := server.APIURL + "/form/" + db.UUIDToString(form.ExternalID)
	if !strings.Contains(body, expectedFormURL) {
		t.Fatal("expected form action in integrations snippet")
	}
	if !strings.Contains(body, db.UUIDToSiteKey(property.ExternalID)) {
		t.Fatal("expected property sitekey in integrations snippet")
	}
	if strings.Contains(body, "On the server") {
		t.Fatal("did not expect server integrations section")
	}
	if strings.Contains(body, "Other") {
		t.Fatal("did not expect other integrations section")
	}
}

func TestPutFormUpdatesSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "settings.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{}
	values.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	values.Set(common.ParamName, form.Name+" updated")
	values.Set(common.ParamURL, "https://hooks.example.com/submit/settings-updated")
	values.Set(common.ParamMethod, http.MethodPut)
	values.Set(common.ParamRetryRequestCount, "on")
	values.Set(common.ParamRequestsPerMinute, "24")
	values.Set(common.ParamActive, "true")

	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/org/%s/form/%s/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(form.ID)), common.EditEndpoint), strings.NewReader(values.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamForm, server.IDHasher.Encrypt(int(form.ID)))

	w := httptest.NewRecorder()
	viewModel, err := server.putForm(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if viewModel == nil {
		t.Fatal("Expected ViewModel, got nil")
	}

	renderCtx, ok := viewModel.Model.(*formSettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *formSettingsRenderContext, got %T", viewModel.Model)
	}
	if renderCtx.SuccessMessage == "" {
		t.Fatal("expected success message after updating form")
	}
	if renderCtx.Form.Name != form.Name+" updated" {
		t.Fatal("expected updated form name in render context")
	}
	if renderCtx.Form.URL != "https://hooks.example.com/submit/settings-updated" {
		t.Fatal("expected updated form URL in render context")
	}
	if renderCtx.Form.Method != http.MethodPut {
		t.Fatalf("expected updated form method %q in render context, got %q", http.MethodPut, renderCtx.Form.Method)
	}
	if renderCtx.Form.RetryRequestCount != 1 {
		t.Fatal("expected updated retry count in render context")
	}
	if renderCtx.Form.RequestsPerMinute != 24 {
		t.Fatal("expected updated requests per minute in render context")
	}

	updatedForm, err := store.Impl().RetrieveOrgForm(ctx, org, form.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve updated form: %v", err)
	}
	if updatedForm.Method != dbgen.FormMethodPut {
		t.Fatalf("expected persisted form method %q, got %q", dbgen.FormMethodPut, updatedForm.Method)
	}
	if updatedForm.RequestsPerMinute != 24 {
		t.Fatalf("expected persisted requests per minute 24, got %d", updatedForm.RequestsPerMinute)
	}
}

func TestPostTestFormReturnsResult(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	receivedMethod := ""
	receivedPath := ""
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "test-form.example.com"),
		db_tests.CreateNewFormParams(user.ID, downstream.URL),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	values := url.Values{}
	values.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	values.Set(common.ParamBody, "email=test@example.com&message=hello")
	values.Set(common.ParamURL, downstream.URL+"/override")
	values.Set(common.ParamMethod, http.MethodDelete)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/%s/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(form.ID)), common.TestEndpoint), strings.NewReader(values.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamForm, server.IDHasher.Encrypt(int(form.ID)))

	w := httptest.NewRecorder()
	viewModel, err := server.postTestForm(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if viewModel == nil {
		t.Fatal("Expected ViewModel, got nil")
	}
	if viewModel.View != "form/settings-test-form.html" {
		t.Fatalf("Expected view %q, got %q", "form/settings-test-form.html", viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*formSettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *formSettingsRenderContext, got %T", viewModel.Model)
	}
	if renderCtx.TestBody != "email=test@example.com&message=hello" {
		t.Fatalf("Expected test body preserved, got %q", renderCtx.TestBody)
	}
	if len(renderCtx.SuccessMessage) == 0 {
		t.Fatal("Expected result to contain success message")
	}
	if receivedMethod != http.MethodPost {
		t.Fatalf("expected downstream method %q, got %q", http.MethodPost, receivedMethod)
	}
	if receivedPath != "/" {
		t.Fatalf("expected downstream path %q, got %q", "/", receivedPath)
	}
	if renderCtx.Form.URL != downstream.URL {
		t.Fatalf("expected rendered test URL %q, got %q", downstream.URL, renderCtx.Form.URL)
	}
	if renderCtx.Form.Method != http.MethodPost {
		t.Fatalf("expected rendered test method %q, got %q", http.MethodPost, renderCtx.Form.Method)
	}
}

func TestPostTestFormReturnsFailureResult(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer downstream.Close()

	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "test-failure.example.com"),
		db_tests.CreateNewFormParams(user.ID, downstream.URL),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	values := url.Values{}
	values.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	values.Set(common.ParamBody, "email=test@example.com")
	values.Set(common.ParamURL, downstream.URL+"/failure")
	values.Set(common.ParamMethod, http.MethodPatch)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/%s/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(form.ID)), common.TestEndpoint), strings.NewReader(values.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamForm, server.IDHasher.Encrypt(int(form.ID)))

	w := httptest.NewRecorder()
	viewModel, err := server.postTestForm(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if viewModel == nil {
		t.Fatal("Expected ViewModel, got nil")
	}

	renderCtx, ok := viewModel.Model.(*formSettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *formSettingsRenderContext, got %T", viewModel.Model)
	}
	if len(renderCtx.WarningMessage) == 0 {
		t.Fatal("Expected result to contain warning message")
	}
}

func TestPostTestFormIgnoresPostedMethodOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	receivedMethod := ""
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "test-invalid-method.example.com"),
		db_tests.CreateNewFormParams(user.ID, downstream.URL),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	values := url.Values{}
	values.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	values.Set(common.ParamBody, "email=test@example.com")
	values.Set(common.ParamURL, downstream.URL+"/invalid")
	values.Set(common.ParamMethod, http.MethodTrace)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/%s/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(form.ID)), common.TestEndpoint), strings.NewReader(values.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamForm, server.IDHasher.Encrypt(int(form.ID)))

	w := httptest.NewRecorder()
	viewModel, err := server.postTestForm(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if viewModel == nil {
		t.Fatal("Expected ViewModel, got nil")
	}

	renderCtx, ok := viewModel.Model.(*formSettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *formSettingsRenderContext, got %T", viewModel.Model)
	}
	if len(renderCtx.SuccessMessage) == 0 {
		t.Fatal("Expected success message")
	}
	if receivedMethod != http.MethodPost {
		t.Fatalf("expected downstream method %q, got %q", http.MethodPost, receivedMethod)
	}
}

func TestPutFormCannotEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	form, _, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(owner.ID, "cannot-edit.example.com"),
		db_tests.CreateNewFormParams(owner.ID, "https://example.com/submit"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{}
	values.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(member.ID))))
	values.Set(common.ParamName, form.Name+" updated")
	values.Set(common.ParamURL, "https://hooks.example.com/submit/member-edit")
	values.Set(common.ParamRetryRequestCount, "on")
	values.Set(common.ParamRequestsPerMinute, "10")
	values.Set(common.ParamActive, "true")

	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/org/%s/form/%s/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(form.ID)), common.EditEndpoint), strings.NewReader(values.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamForm, server.IDHasher.Encrypt(int(form.ID)))

	w := httptest.NewRecorder()
	viewModel, err := server.putForm(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if viewModel == nil {
		t.Fatal("Expected ViewModel, got nil")
	}

	renderCtx, ok := viewModel.Model.(*formSettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *formSettingsRenderContext, got %T", viewModel.Model)
	}
	if renderCtx.ErrorMessage == "" {
		t.Fatal("expected permission error message")
	}

	forms, _, err := store.Impl().RetrieveOrgForms(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 || forms[0].ID != form.ID {
		t.Fatal("form state changed despite permission failure")
	}
}

func TestMoveForm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, property, _, err := server.Store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(org1.UserID.Int32, "move-form.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit"),
		org1)
	if err != nil {
		t.Fatalf("Failed to create new form: %v", err)
	}

	org2, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{}
	values.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	values.Set(common.ParamOrg, server.IDHasher.Encrypt(int(org2.ID)))

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/%s/move", server.IDHasher.Encrypt(int(org1.ID)), server.IDHasher.Encrypt(int(form.ID))), strings.NewReader(values.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}
	location, err := resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect location, got %v", err)
	}
	expectedLocation := fmt.Sprintf("/org/%s/form/%s", server.IDHasher.Encrypt(int(org2.ID)), server.IDHasher.Encrypt(int(form.ID)))
	if location.String() != expectedLocation {
		t.Fatalf("expected redirect to %q, got %q", expectedLocation, location.String())
	}

	forms, _, err := store.Impl().RetrieveOrgForms(ctx, org2, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 || forms[0].ID != form.ID {
		t.Fatal("form was not moved")
	}

	properties, _, err := store.Impl().RetrieveOrgProperties(ctx, org2, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(properties) != 1 || properties[0].ID != property.ID {
		t.Fatal("backing property was not moved")
	}
}

func TestMoveFormInvalidPathArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, _, _, err := server.Store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "move-form-invalid.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://hooks.example.com/submit/move-form-invalid"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	formID := server.IDHasher.Encrypt(int(form.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		path     string
		formBody url.Values
		wantCode int
	}{
		{
			name: "MoveFormInvalidOrgParam",
			path: fmt.Sprintf("/org/%s/form/%s/move", orgID, formID),
			formBody: url.Values{
				common.ParamOrg: {"invalid-org-id"},
			},
			wantCode: http.StatusSeeOther,
		},
		{
			name: "MoveFormToSameOrg",
			path: fmt.Sprintf("/org/%s/form/%s/move", orgID, formID),
			formBody: url.Values{
				common.ParamOrg: {orgID},
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "MoveFormMissingOrgParam",
			path:     fmt.Sprintf("/org/%s/form/%s/move", orgID, formID),
			formBody: url.Values{},
			wantCode: http.StatusSeeOther,
		},
		{
			name: "MoveFormNonexistentOrg",
			path: fmt.Sprintf("/org/%s/form/%s/move", orgID, formID),
			formBody: url.Values{
				common.ParamOrg: {server.IDHasher.Encrypt(999999)},
			},
			wantCode: http.StatusSeeOther,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.formBody.Set(common.ParamCSRFToken, csrfToken)

			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.formBody.Encode()))
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestMoveFormCannotMove(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	form, _, _, err := server.Store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(owner.ID, "move-form-perms.example.com"),
		db_tests.CreateNewFormParams(owner.ID, "https://hooks.example.com/submit/move-form-perms"),
		org1)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org1, member); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Impl().JoinOrg(ctx, org1.ID, member); err != nil {
		t.Fatal(err)
	}
	org2, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", owner.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{}
	values.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(member.ID))))
	values.Set(common.ParamOrg, server.IDHasher.Encrypt(int(org2.ID)))

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/%s/move", server.IDHasher.Encrypt(int(org1.ID)), server.IDHasher.Encrypt(int(form.ID))), strings.NewReader(values.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("Unexpected status code %v", w.Code)
	}
}

func TestDeleteForm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, _, _, err := server.Store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "delete-form.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://hooks.example.com/submit/delete-form"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/org/%s/form/%s/delete", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(form.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	forms, _, err := store.Impl().RetrieveOrgForms(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 0 {
		t.Fatal("form should have been deleted")
	}

	properties, _, err := store.Impl().RetrieveOrgProperties(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(properties) != 0 {
		t.Fatal("backing property should have been deleted")
	}
}

func TestDeleteFormCannotDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	form, _, _, err := server.Store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(owner.ID, "delete-form-restrict.example.com"),
		db_tests.CreateNewFormParams(owner.ID, "https://hooks.example.com/submit/delete-form-restrict"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}
	_, err = store.Impl().InviteUserToOrg(ctx, owner, org, member)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Impl().JoinOrg(ctx, org.ID, member)
	if err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/org/%s/form/%s/delete", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(form.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(member.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("Expected redirect status (303), got %d", w.Code)
	}
}

func TestGetFormDashboardAuditLogs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, _, _, err := server.Store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "audit-form.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://hooks.example.com/submit/audit-form"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/org/%s/form/%s?tab=%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(form.ID)), common.EventsEndpoint), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Unexpected status code %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Audit logs") {
		t.Fatal("expected audit logs view")
	}
	if !strings.Contains(body, "See all Audit Logs") {
		t.Fatal("expected see all audit logs action")
	}
}

type rejectPortalFormURLVerifier struct {
	err error
}

type formsLimitSubscriptionStub struct {
	db.StubSubscriptionLimits
	err error
}

func (v rejectPortalFormURLVerifier) VerifyURL(ctx context.Context, rawURL string) error {
	return v.err
}

func (v rejectPortalFormURLVerifier) VerifyResolvedAddress(ctx context.Context, host string, ip netip.Addr) error {
	return v.err
}

func (v rejectPortalFormURLVerifier) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && (transport != nil) {
		return transport.DialContext(ctx, network, address)
	}

	panic("not configured")
}

func (s formsLimitSubscriptionStub) CheckFormsLimit(ctx context.Context, orgID int32, subscr *dbgen.Subscription) (bool, int, error) {
	return false, 1, s.err
}

func TestPostNewOrgFormInvalidInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	type testCase struct {
		name           string
		setup          func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func())
		mutate         func(values url.Values)
		expectedStatus int
		expectedBody   string
		expectedCount  int
		assertRedirect bool
	}

	newValues := func(userID int32) url.Values {
		values := url.Values{}
		values.Set(common.ParamCSRFToken, server.XSRF.Token(fmt.Sprintf("%d", userID)))
		values.Set(common.ParamName, "Negative Form")
		values.Set(common.ParamDomain, "example.com")
		values.Set(common.ParamURL, "https://hooks.example.com/submit")
		values.Set(common.ParamIgnoreError, "true")
		return values
	}

	testCases := []testCase{
		{
			name: "EmptyName",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			mutate: func(values url.Values) {
				values.Set(common.ParamName, "")
			},
			expectedStatus: http.StatusOK,
			expectedBody:   common.StatusPropertyNameEmptyError.String(),
			expectedCount:  0,
		},
		{
			name: "InvalidDomain",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			mutate: func(values url.Values) {
				values.Del(common.ParamIgnoreError)
				values.Set(common.ParamDomain, "localhost")
			},
			expectedStatus: http.StatusOK,
			expectedBody:   common.StatusPropertyDomainLocalhostError.String(),
			expectedCount:  0,
		},
		{
			name: "EmptyURL",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			mutate: func(values url.Values) {
				values.Set(common.ParamURL, "")
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "URL cannot be empty.",
			expectedCount:  0,
		},
		{
			name: "UnsafeURL",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}

				originalVerifier := server.FormURLVerifier
				server.FormURLVerifier = rejectPortalFormURLVerifier{err: errors.New("blocked")}

				return user, org, user.Email, func() {
					server.FormURLVerifier = originalVerifier
				}
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "URL is not valid.",
			expectedCount:  0,
		},
		{
			name: "MissingSubscription",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewBareAccount(ctx, store, t.Name())
				if err != nil {
					t.Fatalf("Failed to create bare account: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			expectedStatus: http.StatusOK,
			expectedBody:   activeSubscriptionForPropertyError,
			expectedCount:  0,
		},
		{
			name: "InvitedUser",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"owner", testPlan)
				if err != nil {
					t.Fatalf("Failed to create owner account: %v", err)
				}
				member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"member", testPlan)
				if err != nil {
					t.Fatalf("Failed to create invited account: %v", err)
				}
				if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
					t.Fatalf("Failed to invite member: %v", err)
				}
				return member, org, member.Email, func() {}
			},
			expectedStatus: http.StatusSeeOther,
			expectedCount:  0,
			assertRedirect: true,
		},
		{
			name: "FormsLimitExceeded",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}

				originalLimits := server.SubscriptionLimits
				server.SubscriptionLimits = formsLimitSubscriptionStub{}

				return user, org, user.Email, func() {
					server.SubscriptionLimits = originalLimits
				}
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "Forms limit reached for current subscription plan.",
			expectedCount:  0,
		},
		{
			name: "DuplicateName",
			setup: func(t *testing.T) (*dbgen.User, *dbgen.Organization, string, func()) {
				ctx := t.Context()
				user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
				if err != nil {
					t.Fatalf("Failed to create account: %v", err)
				}
				_, _, _, err = store.Impl().CreateNewForm(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), &dbgen.CreateFormParams{
					Name:              "Negative Form",
					URL:               "https://hooks.example.com/existing",
					Fields:            []byte(`{}`),
					Enabled:           true,
					RequestsPerMinute: 60,
					RetryRequestCount: 0,
					Method:            dbgen.FormMethodPost,
				}, org)
				if err != nil {
					t.Fatalf("Failed to create existing form: %v", err)
				}
				return user, org, user.Email, func() {}
			},
			expectedStatus: http.StatusOK,
			expectedBody:   common.StatusPropertyNameDuplicateError.String(),
			expectedCount:  1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			user, org, email, cleanup := tc.setup(t)
			defer cleanup()

			srv := http.NewServeMux()
			server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

			cookie, err := portal_tests.AuthenticateSuite(t.Context(), email, srv, server.XSRF, server.Sessions)
			if err != nil {
				t.Fatal(err)
			}

			values := newValues(user.ID)
			if tc.mutate != nil {
				tc.mutate(values)
			}

			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/org/%s/form/new", server.IDHasher.Encrypt(int(org.ID))), strings.NewReader(values.Encode()))
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d", tc.expectedStatus, w.Code)
			}

			if tc.assertRedirect {
				location, err := w.Result().Location()
				if err != nil {
					t.Fatalf("expected redirect location, got %v", err)
				}
				if !strings.HasPrefix(location.String(), "/"+common.ErrorEndpoint) {
					t.Fatalf("expected error redirect, got %q", location.String())
				}
			} else if tc.expectedBody != "" && !strings.Contains(w.Body.String(), tc.expectedBody) {
				t.Fatalf("expected body to contain %q", tc.expectedBody)
			}

			forms, _, err := store.Impl().RetrieveOrgForms(t.Context(), org, 0, db.MaxOrgPropertiesPageSize)
			if err != nil {
				t.Fatal(err)
			}
			if len(forms) != tc.expectedCount {
				t.Fatalf("expected %d forms, got %d", tc.expectedCount, len(forms))
			}
		})
	}
}
