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
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	// Test all time periods
	periods := []struct {
		endpoint string
		period   common.TimePeriod
	}{
		{PeriodEndpointToday, common.TimePeriodToday},
		{PeriodEndpointWeek, common.TimePeriodWeek},
		{PeriodEndpointMonth, common.TimePeriodMonth},
		{PeriodEndpointYear, common.TimePeriodYear},
	}

	for _, p := range periods {
		t.Run(p.endpoint, func(t *testing.T) {
			req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/stats/%s",
				server.IDHasher.Encrypt(int(org.ID)),
				server.IDHasher.Encrypt(int(property.ID)),
				p.endpoint), nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Unexpected status code %v for period %s", resp.StatusCode, p.endpoint)
			}

			var stats propertyStatsResponse
			if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
				t.Fatalf("Failed to decode response for period %s: %v", p.endpoint, err)
			}

			// All periods should have data since records are recent and included in all periods
			if len(stats.Requested) == 0 {
				t.Errorf("Expected requested data but got none for %s period", p.endpoint)
			}

			if len(stats.Verified) == 0 {
				t.Errorf("Expected verified data but got none for %s period", p.endpoint)
			}

			totalRequested := 0
			for _, pt := range stats.Requested {
				totalRequested += pt.Value
			}

			totalVerified := 0
			for _, pt := range stats.Verified {
				totalVerified += pt.Value
			}

			if totalRequested != 2 {
				t.Errorf("Expected 2 total requested for %s period, got %d", p.endpoint, totalRequested)
			}

			if totalVerified != 2 {
				t.Errorf("Expected 2 total verified for %s period, got %d", p.endpoint, totalVerified)
			}
		})
	}
}

func TestGetPropertyRuleStats(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	// Insert access records with rule IDs
	accessRecords := []*common.AccessRecord{
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			RuleID:     1, // Fake rule ID
			Timestamp:  now.Add(-1 * time.Hour),
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			RuleID:     2, // Another fake rule ID
			Timestamp:  now.Add(-2 * time.Hour),
		},
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			RuleID:     0, // Should be filtered out
			Timestamp:  now.Add(-3 * time.Hour),
		},
	}

	if err := timeSeries.WriteAccessLogBatch(ctx, accessRecords); err != nil {
		t.Fatalf("Failed to write access log batch: %v", err)
	}

	// Give the time series database a moment to process the writes
	time.Sleep(100 * time.Millisecond)

	// Test supported periods (week and month only)
	periods := []struct {
		endpoint string
		period   common.TimePeriod
	}{
		{PeriodEndpointWeek, common.TimePeriodWeek},
		{PeriodEndpointMonth, common.TimePeriodMonth},
	}

	for _, p := range periods {
		t.Run(p.endpoint, func(t *testing.T) {
			req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/rulestats/%s",
				server.IDHasher.Encrypt(int(org.ID)),
				server.IDHasher.Encrypt(int(property.ID)),
				p.endpoint), nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Unexpected status code %v for period %s", resp.StatusCode, p.endpoint)
			}

			var stats propertyRuleStatsResponse
			if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
				t.Fatalf("Failed to decode response for period %s: %v", p.endpoint, err)
			}

			// Should have data since we inserted records with rule_id > 0
			if len(stats.Usage) == 0 {
				t.Errorf("Expected usage data but got none for %s period", p.endpoint)
			}

			totalUsage := 0
			for _, pt := range stats.Usage {
				totalUsage += pt.Value
			}

			// Should have 2 records (the ones with rule_id > 0)
			if totalUsage != 2 {
				t.Errorf("Expected 2 total usage for %s period, got %d", p.endpoint, totalUsage)
			}
		})
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
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

func TestGrowthLevelToIndex(t *testing.T) {
	tests := []struct {
		level    dbgen.DifficultyGrowth
		expected int
	}{
		{dbgen.DifficultyGrowthConstant, 0},
		{dbgen.DifficultyGrowthSlow, 1},
		{dbgen.DifficultyGrowthMedium, 2},
		{dbgen.DifficultyGrowthFast, 3},
		{dbgen.DifficultyGrowth("unknown"), 2},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			result := growthLevelToIndex(tt.level)
			if result != tt.expected {
				t.Errorf("growthLevelToIndex(%s) = %d, want %d", tt.level, result, tt.expected)
			}
		})
	}
}

func TestGrowthLevelFromIndex(t *testing.T) {
	ctx := t.Context()
	tests := []struct {
		index    string
		expected dbgen.DifficultyGrowth
	}{
		{"0", dbgen.DifficultyGrowthConstant},
		{"1", dbgen.DifficultyGrowthSlow},
		{"2", dbgen.DifficultyGrowthMedium},
		{"3", dbgen.DifficultyGrowthFast},
		{"99", dbgen.DifficultyGrowthMedium},
		{"-1", dbgen.DifficultyGrowthMedium},
		{"invalid", dbgen.DifficultyGrowthMedium},
	}

	for _, tt := range tests {
		t.Run(tt.index, func(t *testing.T) {
			result := growthLevelFromValue(ctx, tt.index)
			if result != tt.expected {
				t.Errorf("growthLevelFromValue(%s) = %s, want %s", tt.index, result, tt.expected)
			}
		})
	}
}

func TestParseMaxReplayCount(t *testing.T) {
	ctx := t.Context()
	tests := []struct {
		value    string
		expected int32
	}{
		{"1", 1},
		{"100", 100},
		{"1000000", 1000000},
		{"0", 1},
		{"-1", 1},
		{"2000000", 1000000},
		{"invalid", 1},
		{"", 1},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			result := parseMaxReplayCount(ctx, tt.value)
			if result != tt.expected {
				t.Errorf("parseMaxReplayCount(%s) = %d, want %d", tt.value, result, tt.expected)
			}
		})
	}
}

func TestDifficultyLevelFromValue(t *testing.T) {
	ctx := t.Context()
	tests := []struct {
		value    string
		minLevel int
		maxLevel int
		expected common.DifficultyLevel
	}{
		{"5", 1, 10, 5},
		{"1", 1, 10, 1},
		{"10", 1, 10, 10},
		{"0", 1, 10, common.DifficultyLevelMedium},
		{"-1", 1, 10, common.DifficultyLevelMedium},
		{"invalid", 1, 10, common.DifficultyLevelMedium},
		{"3", 5, 10, 5},
		{"15", 1, 10, 10},
		{"50", 1, 10, 10},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			result := difficultyLevelFromValue(ctx, tt.value, tt.minLevel, tt.maxLevel)
			if result != tt.expected {
				t.Errorf("difficultyLevelFromValue(%s, %d, %d) = %d, want %d", tt.value, tt.minLevel, tt.maxLevel, result, tt.expected)
			}
		})
	}
}

func TestEchoPuzzle(t *testing.T) {
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

	req := httptest.NewRequest("GET", "/echopuzzle/5", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestPropertyEndpointsInvalidPathArg(t *testing.T) {
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

	orgID := server.IDHasher.Encrypt(int(org.ID))

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}
	propertyID := server.IDHasher.Encrypt(int(prop.ID))

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
	}{
		{"GetPropertyDashboardInvalidProperty", "GET", fmt.Sprintf("/org/%s/property/invalid-id", orgID), http.StatusSeeOther},
		{"GetPropertySettingsInvalidProperty", "GET", fmt.Sprintf("/org/%s/property/invalid-id/tab/settings", orgID), http.StatusSeeOther},
		{"GetPropertyReportsInvalidProperty", "GET", fmt.Sprintf("/org/%s/property/invalid-id/tab/reports", orgID), http.StatusSeeOther},
		{"GetPropertyIntegrationsInvalidProperty", "GET", fmt.Sprintf("/org/%s/property/invalid-id/tab/integrations", orgID), http.StatusSeeOther},
		{"GetPropertyAuditLogsInvalidProperty", "GET", fmt.Sprintf("/org/%s/property/invalid-id/tab/events", orgID), http.StatusSeeOther},
		{"GetPropertyStatsInvalidProperty", "GET", fmt.Sprintf("/org/%s/property/invalid-id/stats/24h", orgID), http.StatusBadRequest},
		{"GetPropertyRuleStatsInvalidProperty", "GET", fmt.Sprintf("/org/%s/property/invalid-id/rulestats/7d", orgID), http.StatusBadRequest},
		{"GetPropertyNewRuleInvalidProperty", "GET", fmt.Sprintf("/org/%s/property/invalid-id/rules/new", orgID), http.StatusSeeOther},
		{"PostPropertyNewRuleInvalidProperty", "POST", fmt.Sprintf("/org/%s/property/invalid-id/rules/new", orgID), http.StatusSeeOther},
		{"GetPropertyEditRuleInvalidRule", "GET", fmt.Sprintf("/org/%s/property/%s/rules/invalid-rule/edit", orgID, propertyID), http.StatusSeeOther},
		{"PostPropertyEditRuleInvalidRule", "POST", fmt.Sprintf("/org/%s/property/%s/rules/invalid-rule/edit", orgID, propertyID), http.StatusSeeOther},
		{"PostPropertyMoveRuleInvalidRule", "POST", fmt.Sprintf("/org/%s/property/%s/rules/invalid-rule/move", orgID, propertyID), http.StatusSeeOther},
		{"DeletePropertyRuleInvalidRule", "DELETE", fmt.Sprintf("/org/%s/property/%s/rules/invalid-rule/delete", orgID, propertyID), http.StatusSeeOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			switch tc.method {
			case "POST":
				form := url.Values{}
				form.Set(common.ParamCSRFToken, csrfToken)
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(form.Encode()))
				req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
			case "DELETE":
				req = httptest.NewRequest(tc.method, tc.path, nil)
				req.Header.Set(common.HeaderCSRFToken, csrfToken)
			default:
				req = httptest.NewRequest(tc.method, tc.path, nil)
			}
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestPropertyEndpointsWrongOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	owner, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, "example-wrong-owner.com"), org1)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
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

	org1ID := server.IDHasher.Encrypt(int(org1.ID))
	propertyID := server.IDHasher.Encrypt(int(property.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user2.ID)))

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		useCSRF  bool
	}{
		{"GetPropertyDashboardWrongOwner", "GET", fmt.Sprintf("/org/%s/property/%s", org1ID, propertyID), http.StatusSeeOther, false},
		{"GetPropertySettingsWrongOwner", "GET", fmt.Sprintf("/org/%s/property/%s/tab/settings", org1ID, propertyID), http.StatusSeeOther, false},
		{"GetPropertyReportsWrongOwner", "GET", fmt.Sprintf("/org/%s/property/%s/tab/reports", org1ID, propertyID), http.StatusSeeOther, false},
		{"GetPropertyRulesWrongOwner", "GET", fmt.Sprintf("/org/%s/property/%s/tab/rules", org1ID, propertyID), http.StatusSeeOther, false},
		{"DeletePropertyWrongOwner", "DELETE", fmt.Sprintf("/org/%s/property/%s/delete", org1ID, propertyID), http.StatusSeeOther, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(cookie)
			if tc.useCSRF {
				req.Header.Set(common.HeaderCSRFToken, csrfToken)
			}

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestPropertyEndpointsMissingSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewBareAccount(ctx, store, t.Name())
	if err != nil {
		t.Fatalf("Failed to create account without subscription: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	t.Run("PostNewPropertyMissingSubscription", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "NewPropertyNoSubscription")
		form.Set(common.ParamDomain, "nosub.example.com")
		form.Set(common.ParamIgnoreError, "true")

		req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/new", orgID), strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK with re-rendered form, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "You need an active subscription to create new properties") {
			t.Error("Expected response to contain subscription requirement message")
		}
	})
}

func TestPropertyEndpointsInvalidFormArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "example-form-test.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	propertyID := server.IDHasher.Encrypt(int(property.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	t.Run("PutPropertyInvalidName", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "")
		form.Set(common.ParamDifficulty, "5")
		form.Set(common.ParamGrowth, "2")

		req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/property/%s/edit", orgID, propertyID), strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d", w.Code)
		}
	})

	t.Run("PostNewPropertyInvalidDomain", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "ValidName")
		form.Set(common.ParamDomain, "localhost")

		req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/new", orgID), strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		body := w.Body.String()
		if !strings.Contains(strings.ToLower(body), "localhost") {
			t.Error("Expected response to mention localhost validation error")
		}
	})

	t.Run("PostNewPropertyEmptyName", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "")
		form.Set(common.ParamDomain, "valid.example.com")
		form.Set(common.ParamIgnoreError, "true")

		req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/new", orgID), strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d", w.Code)
		}
	})
}

func TestMovePropertyInvalidPathArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "move-invalid.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	propertyID := server.IDHasher.Encrypt(int(property.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		path     string
		formBody url.Values
		wantCode int
	}{
		{
			name: "MovePropertyInvalidOrgParam",
			path: fmt.Sprintf("/org/%s/property/%s/move", orgID, propertyID),
			formBody: url.Values{
				common.ParamOrg: {"invalid-org-id"},
			},
			wantCode: http.StatusSeeOther,
		},
		{
			name: "MovePropertyToSameOrg",
			path: fmt.Sprintf("/org/%s/property/%s/move", orgID, propertyID),
			formBody: url.Values{
				common.ParamOrg: {orgID},
			},
			wantCode: http.StatusBadRequest,
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

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestMovePropertyInvalidForm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "move-form-invalid.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	propertyID := server.IDHasher.Encrypt(int(property.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		path     string
		formBody url.Values
		wantCode int
	}{
		{
			name:     "MovePropertyMissingOrgParam",
			path:     fmt.Sprintf("/org/%s/property/%s/move", orgID, propertyID),
			formBody: url.Values{
				// Missing org param
			},
			wantCode: http.StatusSeeOther,
		},
		{
			name: "MovePropertyEmptyOrgParam",
			path: fmt.Sprintf("/org/%s/property/%s/move", orgID, propertyID),
			formBody: url.Values{
				common.ParamOrg: {""},
			},
			wantCode: http.StatusSeeOther,
		},
		{
			name: "MovePropertyNonexistentOrg",
			path: fmt.Sprintf("/org/%s/property/%s/move", orgID, propertyID),
			formBody: url.Values{
				common.ParamOrg: {server.IDHasher.Encrypt(999999)},
			},
			wantCode: http.StatusSeeOther,
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

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

// runOrgMemberPropertyCreationPortalTest is the common test logic for portal property creation by org member
func runOrgMemberPropertyCreationPortalTest(t *testing.T, memberSubscrParams *dbgen.CreateSubscriptionParams) {
	t.Helper()
	ctx := t.Context()

	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	member, _, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name()+"_member", memberSubscrParams)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(member.ID)))
	propertyName := t.Name() + "Property"

	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, propertyName)
	form.Set(common.ParamDomain, "google.com")
	form.Set(common.ParamIgnoreError, "true")

	// Step 1: Verify that non-member cannot create properties in the org
	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/new", orgID), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Portal redirects to error page on failure
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected redirect status for non-member, got %v. Body: %s", resp.StatusCode, w.Body.String())
	}
	location, err := resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect response but got error: %v", err)
	}
	if !strings.Contains(location.String(), "error") {
		t.Fatalf("Expected redirect to error page for non-member, got: %s", location.String())
	}

	// Step 2: Invite member to org
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member to org: %v", err)
	}

	// Step 3: Verify that invited (but not joined) member cannot create properties
	req = httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/new", orgID), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp = w.Result()
	// Portal redirects to error page on failure
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected redirect status for invited but not joined member, got %v. Body: %s", resp.StatusCode, w.Body.String())
	}
	location, err = resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect response but got error: %v", err)
	}
	if !strings.Contains(location.String(), "error") {
		t.Fatalf("Expected redirect to error page for invited but not joined member, got: %s", location.String())
	}

	// Step 4: Member joins the org
	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatalf("Failed for member to join org: %v", err)
	}

	// Step 5: Now member should be able to create properties in org where owner has subscription
	req = httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/new", orgID), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected redirect status code, got %v. Body: %s", resp.StatusCode, w.Body.String())
	}

	location, err = resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect response but got error: %v", err)
	}

	expectedPrefix := fmt.Sprintf("/org/%s/property/", orgID)
	if path := location.String(); !strings.HasPrefix(path, expectedPrefix) {
		t.Errorf("Unexpected redirect path: %s, expected prefix: %s", path, expectedPrefix)
	}

	// Step 6: Verify properties were created by the member
	properties, _, err := store.Impl().RetrieveOrgProperties(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}

	if count := len(properties); count != 1 {
		t.Errorf("Unexpected number of properties in org: %v", count)
	} else {
		if properties[0].Name != propertyName {
			t.Errorf("Unexpected property name: %v", properties[0].Name)
		}
		if properties[0].CreatorID.Int32 != member.ID {
			t.Errorf("Property was not created by member: creatorID=%v, memberID=%v", properties[0].CreatorID.Int32, member.ID)
		}
	}
}

func TestOrgMemberWithExpiredTrialCanCreateProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Create subscription params with expired trial
	subscrParams := db_tests.CreateNewSubscriptionParams(testPlan)
	subscrParams.TrialEndsAt = db.Timestampz(time.Now().UTC().AddDate(0, 0, -7)) // Trial ended 7 days ago

	runOrgMemberPropertyCreationPortalTest(t, subscrParams)
}

func TestOrgMemberWithNilSubscriptionCanCreateProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	runOrgMemberPropertyCreationPortalTest(t, nil)
}

func TestGetPropertyDashboardAllTabs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "tabs-example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	tabs := []struct {
		name string
		tab  string
	}{
		{"Reports", common.ReportsEndpoint},
		{"Integrations", common.IntegrationsEndpoint},
		{"Settings", common.SettingsEndpoint},
		{"Events", common.EventsEndpoint},
		{"Rules", common.RulesEndpoint},
		{"Default", ""},
		{"Unknown", "unknown-tab"},
	}

	for _, tc := range tabs {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID)))
			if tc.tab != "" {
				path += "?" + common.ParamTab + "=" + tc.tab
			}

			req := httptest.NewRequest("GET", path, nil)
			req.AddCookie(cookie)
			req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
			req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

			w := httptest.NewRecorder()
			viewModel, err := server.getPropertyDashboard(w, req)
			if err != nil {
				t.Fatalf("Expected no error for tab '%s', got: %v", tc.tab, err)
			}

			if viewModel == nil {
				t.Fatalf("Expected ViewModel for tab '%s', got nil", tc.tab)
			}

			if viewModel.View != propertyDashboardTemplate {
				t.Errorf("Expected view to be %s for tab '%s', got %s", propertyDashboardTemplate, tc.tab, viewModel.View)
			}
		})
	}
}

func TestNewPropertyAuditLogsArray(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create property
	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "prop-audit.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	// Update property to create more audit logs
	if updated, _, _ := server.Store.Impl().UpdateProperty(ctx, org, user, &dbgen.UpdatePropertyParams{
		ID:               property.ID,
		Name:             "Updated Property",
		Level:            db.Int2(int16(common.DifficultyLevelMedium)),
		Growth:           dbgen.DifficultyGrowthMedium,
		ValidityInterval: 6 * time.Hour,
		AllowSubdomains:  false,
		AllowLocalhost:   false,
		MaxReplayCount:   1,
	}); !updated.Enabled {
		// it's a bit unrelated to this test, but useful
		t.Error("Property was disabled by update")
	}

	// Retrieve property audit logs
	logs, err := store.Impl().RetrievePropertyAuditLogs(ctx, property, 100)
	if err != nil {
		t.Fatalf("Failed to retrieve property audit logs: %v", err)
	}

	if len(logs) == 0 {
		t.Skip("No audit logs found for property - skipping test")
	}

	// Test newPropertyAuditLogs
	result := server.newPropertyAuditLogs(ctx, user, logs)

	if len(result) == 0 {
		t.Error("Expected non-empty result from newPropertyAuditLogs")
	}

	// Verify each log has expected fields
	for i, ul := range result {
		if ul.Time == "" {
			t.Errorf("Audit log %d: Expected Time to be set", i)
		}
		if ul.UserName == "" {
			t.Errorf("Audit log %d: Expected UserName to be set", i)
		}
	}
}

func TestPutPropertyCannotEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create owner
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create property under owner
	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, "edit-restrict.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	// Create non-owner member and add to org
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

	// Authenticate as member (not owner or property creator)
	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(member.ID)))

	// Try to edit property
	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, "Updated Name By Member")
	form.Set(common.ParamDifficulty, "100")
	form.Set(common.ParamGrowth, "2")
	form.Set(common.ParamValidityInterval, "4")

	req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()
	viewModel, err := server.putProperty(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel, got nil")
	}

	// Should have error message about permissions
	renderCtx, ok := viewModel.Model.(*propertySettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *propertySettingsRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.ErrorMessage == "" {
		t.Error("Expected ErrorMessage to be set for permission denial")
	}
}

func TestPutPropertyChangeDifficulty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "difficulty-test.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	// Change difficulty to a new value
	newDifficulty := int(common.DifficultyLevelSmall) + 10
	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, property.Name)
	form.Set(common.ParamDifficulty, strconv.Itoa(newDifficulty))
	form.Set(common.ParamGrowth, "2")
	form.Set(common.ParamValidityInterval, "4")

	req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))

	w := httptest.NewRecorder()
	viewModel, err := server.putProperty(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel, got nil")
	}

	// Should have success message
	renderCtx, ok := viewModel.Model.(*propertySettingsRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *propertySettingsRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.SuccessMessage == "" {
		t.Error("Expected SuccessMessage to be set after updating property")
	}
}

func TestDeletePropertyCannotDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create owner
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Create property under owner
	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, "delete-restrict.com"), org)
	if err != nil {
		t.Fatalf("Failed to create new property: %v", err)
	}

	// Create non-owner member and add to org
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

	// Authenticate as member (not owner or property creator)
	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(member.ID)))

	// Try to delete property
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Member cannot delete property they don't own - should return 405 Method Not Allowed
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected method not allowed (405), got %d", w.Code)
	}
}

func TestGetOrgPropertyDisabled(t *testing.T) {
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// First verify the property is accessible
	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s",
		server.IDHasher.Encrypt(int(org.ID)),
		server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK before disabling, got %d", w.Code)
	}

	// Now disable the property
	if err := db_tests.DisableProperty(ctx, store, property.ID); err != nil {
		t.Fatal(err)
	}

	// Clear cache so the next request fetches fresh data from DB
	cache.Delete(ctx, db.PropertyByIDCacheKey(property.ID))

	// Try to access the disabled property
	req = httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s",
		server.IDHasher.Encrypt(int(org.ID)),
		server.IDHasher.Encrypt(int(property.ID))), nil)
	req.AddCookie(cookie)

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should redirect to error page (status 303 See Other for forbidden)
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303) for disabled property, got %d", w.Code)
	}

	location, _ := w.Result().Location()
	if location == nil || !strings.Contains(location.String(), common.ErrorEndpoint) {
		t.Errorf("Expected redirect to error endpoint, got %v", location)
	}
}
