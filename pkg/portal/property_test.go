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
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func TestGetNewOrgProperty(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/new", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getNewOrgProperty(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != propertyWizardTemplate {
		t.Errorf("Expected view to be %s, got %s", propertyWizardTemplate, viewModel.View)
	}

	if viewModel.Model == nil {
		t.Fatal("Expected Model to be populated, got nil")
	}

	renderCtx, ok := viewModel.Model.(*propertyWizardRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *propertyWizardRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.CurrentOrg == nil {
		t.Fatal("Expected CurrentOrg to be populated, got nil")
	}

	if renderCtx.CurrentOrg.Name != org.Name {
		t.Errorf("Expected org name to be %s, got %s", org.Name, renderCtx.CurrentOrg.Name)
	}

	if renderCtx.CurrentOrg.ID != server.IDHasher.Encrypt(int(org.ID)) {
		t.Errorf("Expected org ID to be %s, got %s", server.IDHasher.Encrypt(int(org.ID)), renderCtx.CurrentOrg.ID)
	}

	if len(renderCtx.Token) == 0 {
		t.Error("Expected CSRF token to be populated")
	}
}

func TestPutPropertyInsufficientPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	_, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_1", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org1.UserID.Int32, "example.com"), org1)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	user2, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_2", testPlan)
	if err != nil {
		t.Fatalf("Failed to create intruder account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user2.ID))))
	form.Set(common.ParamName, "Updated Property Name")
	form.Set(common.ParamDifficulty, "0")
	form.Set(common.ParamGrowth, "2")

	req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/property/%s/edit", server.IDHasher.Encrypt(int(org1.ID)), server.IDHasher.Encrypt(int(property.ID))),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	url, _ := resp.Location()
	if path := url.String(); !strings.HasPrefix(path, "/"+common.ErrorEndpoint) {
		t.Errorf("Unexpected redirect: %s", path)
	}
}

func TestPostNewOrgProperty(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	propertyName := t.Name() + "Property"

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, propertyName)
	form.Set(common.ParamDomain, "google.com")

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/new", server.IDHasher.Encrypt(int(org.ID))),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect response but got error: %v", err)
	}

	expectedPrefix := fmt.Sprintf("/org/%s/property/", server.IDHasher.Encrypt(int(org.ID)))
	if path := location.String(); !strings.HasPrefix(path, expectedPrefix) {
		t.Errorf("Unexpected redirect path: %s, expected prefix: %s", path, expectedPrefix)
	}

	pp, _, err := store.Impl().RetrieveOrgProperties(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}

	if count := len(pp); count != 1 {
		t.Errorf("Unexpected number of properties in org: %v", count)
	} else {
		if pp[0].Name != propertyName {
			t.Errorf("Unexpected property in org: %v", pp[0].Name)
		}
	}
}

func TestMoveProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org1.UserID.Int32, "example.com"), org1)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	org2, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-another-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create extra org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamOrg, server.IDHasher.Encrypt(int(org2.ID)))

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/%s/move", server.IDHasher.Encrypt(int(org1.ID)), server.IDHasher.Encrypt(int(property.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	properties, _, err := store.Impl().RetrieveOrgProperties(ctx, org2, 0, db.MaxOrgPropertiesPageSize)
	if len(properties) != 1 || properties[0].ID != property.ID {
		t.Errorf("Property was not moved")
	}
}

func TestRetrieveProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	for i := 0; i < 3*db.MaxOrgPropertiesPageSize/2; i++ {
		if _, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, fmt.Sprintf("example%v.com", i)), org); err != nil {
			t.Fatalf("Failed to create new property: %v", err)
		}
	}

	testCases := []struct {
		offset   int
		count    int
		expected int
		hasMore  bool
	}{
		{0, db.MaxOrgPropertiesPageSize, db.MaxOrgPropertiesPageSize, true},
		{0, 1, 1, true},
		{0, db.MaxOrgPropertiesPageSize * 100, db.MaxOrgPropertiesPageSize, true},
		{db.MaxOrgPropertiesPageSize, db.MaxOrgPropertiesPageSize, db.MaxOrgPropertiesPageSize / 2, false},
		{db.MaxOrgPropertiesPageSize, db.MaxOrgPropertiesPageSize/2 - 1, db.MaxOrgPropertiesPageSize/2 - 1, true},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("properties_offset_%v_count_%v", tc.offset, tc.count), func(t *testing.T) {
			properties, hasMore, err := server.Store.Impl().RetrieveOrgProperties(ctx, org, tc.offset, tc.count)
			if err != nil {
				t.Fatal(err)
			}

			if actual := len(properties); actual != tc.expected {
				t.Errorf("Received %v properties, but expected %v", actual, tc.expected)
			}

			if hasMore != tc.hasMore {
				t.Errorf("Received %v more, but expected %v", hasMore, tc.hasMore)
			}
		})
	}
}

func TestGetPropertyStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
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
	}

	if err := timeSeries.WriteAccessLogBatch(ctx, accessRecords); err != nil {
		t.Fatalf("Failed to write access log batch: %v", err)
	}

	verifyRecords := []*common.VerifyRecord{
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			PuzzleID:   1,
			Timestamp:  now.Add(-1 * time.Hour),
			Status:     int8(puzzle.VerifyNoError),
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			PuzzleID:   2,
			Timestamp:  now.Add(-2 * time.Hour),
			Status:     int8(puzzle.VerifyNoError),
		},
	}

	if err := timeSeries.WriteVerifyLogBatch(ctx, verifyRecords); err != nil {
		t.Fatalf("Failed to write verify log batch: %v", err)
	}

	// Give the time series database a moment to process the writes
	time.Sleep(100 * time.Millisecond)

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/stats/24h",
		server.IDHasher.Encrypt(int(org.ID)),
		server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected status code %v", resp.StatusCode)
	}

	var stats propertyStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(stats.Requested) == 0 {
		t.Error("Expected requested data but got none")
	}

	if len(stats.Verified) == 0 {
		t.Error("Expected verified data but got none")
	}

	totalRequested := 0
	for _, p := range stats.Requested {
		totalRequested += p.Value
	}

	totalVerified := 0
	for _, p := range stats.Verified {
		totalVerified += p.Value
	}

	if totalRequested != 2 {
		t.Errorf("Expected 2 total requested, got %d", totalRequested)
	}

	if totalVerified != 2 {
		t.Errorf("Expected 2 total verified, got %d", totalVerified)
	}
}

func TestGetOrgProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()

	renderCtx, dbProperty, err := server.getOrgProperty(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if renderCtx == nil {
		t.Fatal("Expected render context to be populated, got nil")
	}

	if dbProperty == nil {
		t.Fatal("Expected property to be populated, got nil")
	}

	if dbProperty.ID != property.ID {
		t.Errorf("Expected property ID to be %d, got %d", property.ID, dbProperty.ID)
	}

	if renderCtx.Property == nil {
		t.Fatal("Expected Property in render context to be populated, got nil")
	}

	if !renderCtx.CanEdit {
		t.Error("Expected CanEdit to be true for property creator")
	}
}

func TestGetOrgPropertySettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/tab/settings", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()

	renderCtx, auditEvent, err := server.getOrgPropertySettings(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if renderCtx == nil {
		t.Fatal("Expected render context to be populated, got nil")
	}

	if renderCtx.Tab != propertySettingsTabIndex {
		t.Errorf("Expected tab to be %d, got %d", propertySettingsTabIndex, renderCtx.Tab)
	}

	if auditEvent == nil {
		t.Error("Expected audit event to be populated")
	}
}

func TestGetPropertyDashboard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getPropertyDashboard(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != propertyDashboardTemplate {
		t.Errorf("Expected view to be %s, got %s", propertyDashboardTemplate, viewModel.View)
	}
}

func TestGetPropertyReportsTab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/tab/reports", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getPropertyReportsTab(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != propertyDashboardReportsTemplate {
		t.Errorf("Expected view to be %s, got %s", propertyDashboardReportsTemplate, viewModel.View)
	}
}

func TestGetPropertySettingsTab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/tab/settings", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getPropertySettingsTab(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != propertyDashboardSettingsTemplate {
		t.Errorf("Expected view to be %s, got %s", propertyDashboardSettingsTemplate, viewModel.View)
	}

	if viewModel.AuditEvent == nil {
		t.Error("Expected AuditEvent to be populated")
	}
}

func TestGetPropertyIntegrationsTab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/tab/integrations", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getPropertyIntegrationsTab(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != propertyDashboardIntegrationsTemplate {
		t.Errorf("Expected view to be %s, got %s", propertyDashboardIntegrationsTemplate, viewModel.View)
	}
}

func TestGetPropertyAuditLogsTab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/tab/events", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getPropertyAuditLogsTab(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != propertyDashboardAuditLogsTemplate {
		t.Errorf("Expected view to be %s, got %s", propertyDashboardAuditLogsTemplate, viewModel.View)
	}
}

func TestDeleteProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/property/%s/delete", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	properties, _, err := store.Impl().RetrieveOrgProperties(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}

	if len(properties) != 0 {
		t.Error("Property should have been deleted")
	}
}
