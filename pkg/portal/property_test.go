package portal

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPropertyToUserPropertyDisplayDomain(t *testing.T) {
	tests := []struct {
		name            string
		domain          string
		expected        string
		allowSubdomains bool
		hasDomain       bool
	}{
		{name: "EmptyDomain", domain: "", allowSubdomains: false, expected: "any domain (*)", hasDomain: false},
		{name: "EmptyDomainWithSubdomains", domain: "", allowSubdomains: true, expected: "any domain (*)", hasDomain: false},
		{name: "Subdomains", domain: "example.com", allowSubdomains: true, expected: "*.example.com", hasDomain: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			property := &dbgen.Property{
				ID:               1,
				OrgID:            db.Int(2),
				Domain:           tt.domain,
				AllowSubdomains:  tt.allowSubdomains,
				Level:            db.Int2(1),
				Growth:           dbgen.DifficultyGrowthMedium,
				ValidityInterval: 6 * time.Hour,
				MaxReplayCount:   1,
				ExternalID:       pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
			}

			result := propertyToUserProperty(property, server.IDHasher)
			if result.Domain != tt.expected {
				t.Errorf("Domain = %q; want %q", result.Domain, tt.expected)
			}
			if result.HasDomain != tt.hasDomain {
				t.Errorf("HasDomain = %v; want %v", result.HasDomain, tt.hasDomain)
			}
		})
	}
}

func TestPropertyToUserPropertyEdgeValidity(t *testing.T) {
	property := &dbgen.Property{ID: 1, OrgID: db.Int(2)}
	for i, duration := range puzzle.ValidityDurations {
		property.EdgeTokenValidityInterval = duration
		if got := propertyToUserProperty(property, server.IDHasher).EdgeTokenValidityInterval; got != i+1 {
			t.Errorf("exact duration %v: got index %d, want %d", duration, got, i+1)
		}
	}
	for _, tt := range []struct {
		period time.Duration
		want   int
	}{
		{0, 0},
		{-time.Minute, 0},
		{time.Minute, 1},
		{8 * time.Minute, 2},
		{20 * time.Minute, 2},
		{25 * time.Minute, 3},
		{2 * time.Hour, 4},
		{5 * time.Hour, 5},
		{48 * time.Hour, 7},
	} {
		property.EdgeTokenValidityInterval = tt.period
		if got := propertyToUserProperty(property, server.IDHasher).EdgeTokenValidityInterval; got != tt.want {
			t.Errorf("duration %v: got index %d, want %d", tt.period, got, tt.want)
		}
	}
}

func TestCanEditProperty(t *testing.T) {
	org := &dbgen.Organization{UserID: db.Int(1)}
	property := &dbgen.Property{CreatorID: db.Int(2)}

	tests := []struct {
		name string
		user *dbgen.User
		want bool
	}{
		{name: "org owner can edit", user: &dbgen.User{ID: 1}, want: true},
		{name: "property creator can edit", user: &dbgen.User{ID: 2}, want: true},
		{name: "org member cannot edit", user: &dbgen.User{ID: 3}, want: false},
		{name: "nil user cannot edit", user: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canEditProperty(tt.user, org, property); got != tt.want {
				t.Fatalf("canEditProperty() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	pp, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
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

	properties, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org2, 0, db.MaxOrgPropertiesPageSize)
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
			properties, hasMore, err := server.Store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, tc.offset, tc.count)
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

func TestRetrievePropertiesSorted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}

	properties := make([]*dbgen.Property, 0, 3)
	for _, name := range []string{"Zulu", "Alpha", "Middle"} {
		params := db_tests.CreateNewPropertyParams(user.ID, strings.ToLower(name)+".example.com")
		params.Name = name
		property, _, err := server.Store.Impl().CreateNewProperty(ctx, params, org)
		if err != nil {
			t.Fatal(err)
		}
		properties = append(properties, property)
	}

	tests := []struct {
		name string
		sort db.OrgPropertiesSort
		want []int32
	}{
		{name: "DateAscending", sort: db.OrgPropertiesSortDateAscending, want: []int32{properties[0].ID, properties[1].ID, properties[2].ID}},
		{name: "DateDescending", sort: db.OrgPropertiesSortDateDescending, want: []int32{properties[2].ID, properties[1].ID, properties[0].ID}},
		{name: "NameAscending", sort: db.OrgPropertiesSortNameAscending, want: []int32{properties[1].ID, properties[2].ID, properties[0].ID}},
		{name: "NameDescending", sort: db.OrgPropertiesSortNameDescending, want: []int32{properties[0].ID, properties[2].ID, properties[1].ID}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, _, err := server.Store.Impl().RetrieveOrgProperties(ctx, org, tt.sort, 0, db.MaxOrgPropertiesPageSize)
			if err != nil {
				t.Fatal(err)
			}
			if len(actual) != len(tt.want) {
				t.Fatalf("got %d properties, want %d", len(actual), len(tt.want))
			}
			for i, property := range actual {
				if property.ID != tt.want[i] {
					t.Errorf("property %d ID = %d, want %d", i, property.ID, tt.want[i])
				}
			}
		})
	}

	actual, hasMore, err := server.Store.Impl().RetrieveOrgProperties(ctx, org, db.OrgPropertiesSortNameAscending, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != 1 || actual[0].ID != properties[2].ID {
		t.Fatalf("unexpected second name-sorted property: %#v", actual)
	}
	if !hasMore {
		t.Fatal("expected a third name-sorted property")
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
	verifyOnlyTime := now.Add(-3 * time.Hour)
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
		{
			UserID:     user.ID,
			OrgID:      org.ID,
			PropertyID: property.ID,
			PuzzleID:   3,
			Timestamp:  verifyOnlyTime,
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

			var stats PropertyStatsResponse
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

			if totalVerified != 3 {
				t.Errorf("Expected 3 total verified for %s period, got %d", p.endpoint, totalVerified)
			}

			if p.period == common.TimePeriodToday {
				verifyOnlyBucket := verifyOnlyTime.Truncate(time.Hour).Unix()
				requestedByDate := make(map[int64]int, len(stats.Requested))
				verifiedByDate := make(map[int64]int, len(stats.Verified))
				for _, pt := range stats.Requested {
					requestedByDate[pt.Date] = pt.Value
				}
				for _, pt := range stats.Verified {
					verifiedByDate[pt.Date] = pt.Value
				}

				if requested, ok := requestedByDate[verifyOnlyBucket]; !ok || requested != 0 {
					t.Errorf("Expected 0 requested in verify-only bucket, got %d (present: %v)", requested, ok)
				}
				if verified, ok := verifiedByDate[verifyOnlyBucket]; !ok || verified != 1 {
					t.Errorf("Expected 1 verified in verify-only bucket, got %d (present: %v)", verified, ok)
				}
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

			var stats PropertyRuleStatsResponse
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

	params := db_tests.CreateNewPropertyParams(user.ID, "example.com")
	params.EdgeTokenValidityInterval = time.Hour
	property, _, err := server.Store.Impl().CreateNewProperty(ctx, params, org)
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
	if renderCtx.EdgeWidgetStartMode != "click" {
		t.Fatalf("missing edge settings did not use defaults: %q", renderCtx.EdgeWidgetStartMode)
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

	if len(viewModel.AuditEvents) == 0 {
		t.Error("Expected AuditEvents to be populated")
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

	properties, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
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

type echoPuzzleCapture struct {
	*portal_tests.StubPuzzleEngine
	puzzle puzzle.Puzzle
}

func (e *echoPuzzleCapture) Write(_ context.Context, p puzzle.Puzzle, _ []byte, _ http.ResponseWriter) error {
	e.puzzle = p
	return nil
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
	previousEngine := server.PuzzleEngine
	capture := &echoPuzzleCapture{StubPuzzleEngine: &portal_tests.StubPuzzleEngine{}}
	server.PuzzleEngine = capture
	t.Cleanup(func() { server.PuzzleEngine = previousEngine })

	req := httptest.NewRequest("GET", "/echopuzzle/5", nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if capture.puzzle == nil {
		t.Fatal("portal echo issued no puzzle")
	}
	body, err := capture.puzzle.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 47 || body[0] != 1 || capture.puzzle.Challenge() != puzzle.ChallengeBlake2b || !capture.puzzle.IsStub() {
		t.Fatalf("portal echo puzzle = %+v, bytes = %x; want Blake v1 stub", capture.puzzle, body)
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
		req.SetPathValue(common.ParamOrg, orgID)

		w := httptest.NewRecorder()
		viewModel, err := server.postNewOrgProperty(w, req)
		if err != nil {
			t.Fatal(err)
		}
		if viewModel == nil {
			t.Fatal("Expected ViewModel, got nil")
		}

		renderCtx, ok := viewModel.Model.(*propertyWizardRenderContext)
		if !ok {
			t.Fatalf("Expected *propertyWizardRenderContext, got %T", viewModel.Model)
		}
		if renderCtx.ErrorMessage != activeSubscriptionForPropertyError {
			t.Errorf("Expected subscription error %q, got %q", activeSubscriptionForPropertyError, renderCtx.ErrorMessage)
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
		req.SetPathValue(common.ParamOrg, orgID)

		w := httptest.NewRecorder()
		viewModel, err := server.postNewOrgProperty(w, req)
		if err != nil {
			t.Fatal(err)
		}
		if viewModel == nil {
			t.Fatal("Expected ViewModel, got nil")
		}

		renderCtx, ok := viewModel.Model.(*propertyWizardRenderContext)
		if !ok {
			t.Fatalf("Expected *propertyWizardRenderContext, got %T", viewModel.Model)
		}
		if want := common.StatusPropertyDomainLocalhostError.String(); renderCtx.DomainError != want {
			t.Errorf("Expected domain error %q, got %q", want, renderCtx.DomainError)
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected OK status code, got %v. Body: %s", resp.StatusCode, w.Body.String())
	}

	// Step 6: Verify properties were created by the member
	properties, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
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
	if viewModel.View != propertySettingsBasicFormTemplate {
		t.Fatalf("basic settings view = %q, want %q", viewModel.View, propertySettingsBasicFormTemplate)
	}
	form.Set(common.ParamEdgeTokenValidityInterval, "4")
	form.Set(common.ParamEdgeWidgetStartMode, "load")
	edgeReq := httptest.NewRequest(http.MethodPut, req.URL.Path, strings.NewReader(form.Encode()))
	edgeReq.AddCookie(cookie)
	edgeReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	edgeReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	edgeReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	edgeView, err := server.putPropertyEdgeSettings(httptest.NewRecorder(), edgeReq)
	if err != nil || edgeView.Model.(*edgePropertySettingsRenderContext).ErrorMessage == "" {
		t.Fatalf("member was allowed to edit edge settings: %+v, err=%v", edgeView, err)
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
	if viewModel.View != propertySettingsBasicFormTemplate {
		t.Fatalf("basic settings view = %q, want %q", viewModel.View, propertySettingsBasicFormTemplate)
	}
}

func TestPutPropertyRenameOnlyPreservesOutOfDifficultyRange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cases := []struct {
		name        string
		storedLevel int16
		submitDiff  string
		newName     string
		wantLevel   int16
		wantUpdate  bool
	}{
		{name: "HighBandRenameOnly", storedLevel: 220, submitDiff: "200", newName: "renamed", wantLevel: 220, wantUpdate: true},
		{name: "LowBandRenameOnly", storedLevel: 72, submitDiff: "136", newName: "renamed", wantLevel: 72, wantUpdate: true},
		{name: "HighBandDifficultyChange", storedLevel: 220, submitDiff: "180", newName: "renamed", wantLevel: 180, wantUpdate: true},
		{name: "InRangeRenameOnly", storedLevel: 168, submitDiff: "168", newName: "renamed", wantLevel: 168, wantUpdate: true},
		{name: "HighBandNoChange", storedLevel: 220, submitDiff: "200", newName: "", wantLevel: 220, wantUpdate: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			user, org, err := db_tests.CreateNewAccountForTest(ctx, store, "PutPropertyRange"+tc.name, testPlan)
			if err != nil {
				t.Fatalf("Failed to create account: %v", err)
			}

			params := db_tests.CreateNewPropertyParams(user.ID, "difficulty-clamp-"+tc.name+".com")
			params.Level = db.Int2(tc.storedLevel)
			property, _, err := server.Store.Impl().CreateNewProperty(ctx, params, org)
			if err != nil {
				t.Fatalf("Failed to create new property: %v", err)
			}
			if property.Level.Int16 != tc.storedLevel {
				t.Fatalf("Setup failed: stored Level = %d, want %d", property.Level.Int16, tc.storedLevel)
			}

			srv := http.NewServeMux()
			server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
			cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
			if err != nil {
				t.Fatal(err)
			}
			csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

			submitName := property.Name
			if tc.newName != "" {
				submitName = property.Name + "-" + tc.newName
			}

			form := url.Values{}
			form.Set(common.ParamCSRFToken, csrfToken)
			form.Set(common.ParamName, submitName)
			form.Set(common.ParamDifficulty, tc.submitDiff)
			form.Set(common.ParamGrowth, "2")
			form.Set(common.ParamValidityInterval, "4")

			req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/property/%s",
				server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))),
				strings.NewReader(form.Encode()))
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

			renderCtx, ok := viewModel.Model.(*propertySettingsRenderContext)
			if !ok {
				t.Fatalf("Expected Model to be *propertySettingsRenderContext, got %T", viewModel.Model)
			}

			if tc.wantUpdate && renderCtx.SuccessMessage == "" {
				t.Error("Expected SuccessMessage to be set after updating property")
			}
			if !tc.wantUpdate && renderCtx.SuccessMessage != "" {
				t.Errorf("Expected no update, but SuccessMessage was set: %q", renderCtx.SuccessMessage)
			}

			props, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
			if err != nil {
				t.Fatalf("Failed to retrieve properties: %v", err)
			}

			var fetched *dbgen.Property
			for _, p := range props {
				if p.ID == property.ID {
					fetched = p
					break
				}
			}
			if fetched == nil {
				t.Fatalf("Property %d not found after update", property.ID)
			}

			if fetched.Level.Int16 != tc.wantLevel {
				t.Errorf("Stored Level corrupted: got %d, want %d", fetched.Level.Int16, tc.wantLevel)
			}

			if tc.newName != "" {
				wantName := property.Name + "-" + tc.newName
				if fetched.Name != wantName {
					t.Errorf("Property name not renamed: got %q, want %q", fetched.Name, wantName)
				}
			} else if fetched.Name != property.Name {
				t.Errorf("Property name changed unexpectedly: got %q, want %q", fetched.Name, property.Name)
			}
		})
	}
}

func TestPutPropertyEdgeSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "edge-settings.example.com"), org)
	if err != nil {
		t.Fatal(err)
	}
	if property.EdgeTokenValidityInterval != 0 {
		t.Fatalf("default TTL = %v", property.EdgeTokenValidityInterval)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	previous := server.EdgeTokens
	edgeConfig := config.NewBaseConfig(config.NewEnvConfig(func(string) string { return "" }))
	edgeConfig.Add(config.NewStaticValue(common.EdgeTokenSigningPrivateKeyKey, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))))
	edgeConfig.Add(config.NewStaticValue(common.EdgeTokenSigningPublicKeyKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))))
	server.EdgeTokens = api.NewEdgeTokenSigner("", edgeConfig)
	if err := server.EdgeTokens.Update(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { server.EdgeTokens = previous }()
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, property.Name)
	form.Set(common.ParamDifficulty, strconv.Itoa(int(property.Level.Int16)))
	form.Set(common.ParamGrowth, strconv.Itoa(growthLevelToIndex(property.Growth)))
	form.Set(common.ParamValidityInterval, strconv.Itoa(puzzle.ValidityIntervalToIndex(property.ValidityInterval)))
	form.Set(common.ParamEdgeTokenValidityInterval, "4")
	form.Set(common.ParamEdgeWidgetStartMode, "load")
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), strings.NewReader(form.Encode()))
	for _, handler := range []string{"edge", "basic"} {
		t.Run("FeatureDisabled/"+handler, func(t *testing.T) {
			previousFlags := server.FeatureFlags
			server.FeatureFlags = featureFlagsFunc(func(_ context.Context, feature string, userID, orgID *int32) bool {
				if feature != common.FeatureEdgeTokens {
					return true
				}
				if userID == nil || *userID != user.ID || orgID == nil || *orgID != org.ID {
					t.Error("feature check did not receive the authenticated user and property organization")
				}
				return false
			})
			t.Cleanup(func() { server.FeatureFlags = previousFlags })
			disabledReq := httptest.NewRequest(http.MethodPut, req.URL.Path, strings.NewReader(form.Encode()))
			disabledReq.AddCookie(cookie)
			disabledReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
			disabledReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
			disabledReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
			var disabledView *ViewModel
			var err error
			var errorMessage string
			if handler == "edge" {
				disabledView, err = server.putPropertyEdgeSettings(httptest.NewRecorder(), disabledReq)
				if err == nil {
					errorMessage = disabledView.Model.(*edgePropertySettingsRenderContext).ErrorMessage
				}
			} else {
				disabledView, err = server.putProperty(httptest.NewRecorder(), disabledReq)
				if err == nil {
					errorMessage = disabledView.Model.(*propertySettingsRenderContext).ErrorMessage
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if errorMessage != common.StatusPropertyPermissionsError.String() || len(disabledView.AuditEvents) != 0 {
				t.Errorf("disabled edge feature was accepted: %+v", disabledView)
			}
			stored, err := store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
			if err != nil || stored.EdgeTokenValidityInterval != 0 {
				t.Fatalf("disabled feature changed edge lifetime: %+v, err=%v", stored, err)
			}
		})
	}
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	w := httptest.NewRecorder()
	view, err := server.putPropertyEdgeSettings(w, req)
	if err != nil {
		t.Fatal(err)
	}
	model := view.Model.(*edgePropertySettingsRenderContext)
	if model.SuccessMessage == "" || model.Property.EdgeTokenValidityInterval != 4 || model.EdgeWidgetStartMode != "load" {
		t.Fatalf("portal update: %+v", model)
	}
	if view.View != propertySettingsEdgeFormTemplate {
		t.Fatalf("edge settings view = %q, want %q", view.View, propertySettingsEdgeFormTemplate)
	}
	if len(view.AuditEvents) != 2 || view.AuditEvents[1].TableName != db.TableNameEdgeSettings {
		t.Fatalf("edge settings audit events = %+v", view.AuditEvents)
	}
	updated, err := store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil || updated.EdgeTokenValidityInterval != time.Hour || updated.Name != property.Name || updated.Level != property.Level {
		t.Fatalf("stored TTL = %v, err = %v", updated, err)
	}
	edge, err := store.Impl().RetrieveEdgeSettingsBySitekey(ctx, db.UUIDToSiteKey(property.ExternalID), false)
	if err != nil || edge.EdgeWidgetStartMode != "load" {
		t.Fatalf("stored edge settings = %+v, err=%v", edge, err)
	}

	for _, handler := range []string{"edge", "basic"} {
		for _, invalid := range []string{"missing", "", "invalid", "-1", strconv.Itoa(len(puzzle.ValidityDurations) + 1), "+0", "00"} {
			if handler == "basic" && invalid == "missing" {
				continue
			}
			t.Run(handler+" rejects validity "+invalid, func(t *testing.T) {
				form.Set(common.ParamEdgeTokenValidityInterval, invalid)
				if invalid == "missing" {
					form.Del(common.ParamEdgeTokenValidityInterval)
				}
				form.Set(common.ParamEdgeWidgetStartMode, "click")
				invalidReq := httptest.NewRequest(http.MethodPut, req.URL.Path, strings.NewReader(form.Encode()))
				invalidReq.AddCookie(cookie)
				invalidReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
				invalidReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
				invalidReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
				var invalidView *ViewModel
				var err error
				if handler == "edge" {
					invalidView, err = server.putPropertyEdgeSettings(httptest.NewRecorder(), invalidReq)
				} else {
					invalidView, err = server.putProperty(httptest.NewRecorder(), invalidReq)
				}
				if err != nil {
					t.Fatal(err)
				}
				var errorMessage string
				switch model := invalidView.Model.(type) {
				case *edgePropertySettingsRenderContext:
					errorMessage = model.ErrorMessage
				case *propertySettingsRenderContext:
					errorMessage = model.ErrorMessage
				}
				if errorMessage == "" || len(invalidView.AuditEvents) != 0 {
					t.Errorf("malformed validity was accepted: %+v", invalidView)
				}
				stored, err := store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
				if err != nil || stored.EdgeTokenValidityInterval != time.Hour {
					t.Fatalf("malformed validity changed edge lifetime: %+v, err=%v", stored, err)
				}
				settings, err := store.Impl().RetrieveEdgeSettingsBySitekey(ctx, db.UUIDToSiteKey(property.ExternalID), false)
				if err != nil || settings.EdgeWidgetStartMode != "load" {
					t.Fatalf("malformed validity changed widget settings: %+v, err=%v", settings, err)
				}
			})
		}
	}
	form.Set(common.ParamEdgeTokenValidityInterval, "4")
	form.Set(common.ParamEdgeWidgetStartMode, "auto")
	invalidReq := httptest.NewRequest(http.MethodPut, req.URL.Path, strings.NewReader(form.Encode()))
	invalidReq.AddCookie(cookie)
	invalidReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	invalidReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	invalidReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	view, err = server.putPropertyEdgeSettings(httptest.NewRecorder(), invalidReq)
	if err != nil {
		t.Fatal(err)
	}
	if view.Model.(*edgePropertySettingsRenderContext).ErrorMessage == "" {
		t.Fatal("unsupported start mode was accepted")
	}
	edge, err = store.Impl().RetrieveEdgeSettingsBySitekey(ctx, db.UUIDToSiteKey(property.ExternalID), true)
	if err != nil || edge.EdgeWidgetStartMode != "load" {
		t.Fatalf("unsupported mode changed settings: %+v, err=%v", edge, err)
	}
	form.Del(common.ParamEdgeTokenValidityInterval)
	form.Del(common.ParamEdgeWidgetStartMode)
	form.Set(common.ParamName, property.Name+" renamed")
	basicReq := httptest.NewRequest(http.MethodPut, req.URL.Path, strings.NewReader(form.Encode()))
	basicReq.AddCookie(cookie)
	basicReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	basicReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	basicReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	if _, err := server.putProperty(httptest.NewRecorder(), basicReq); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil || stored.EdgeTokenValidityInterval != time.Hour {
		t.Fatalf("basic settings changed edge validity: %+v, err=%v", stored, err)
	}
	form.Set(common.ParamEdgeTokenValidityInterval, "0")
	form.Set(common.ParamEdgeWidgetStartMode, "click")
	disableReq := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/org/%s/property/%s/edge", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))),
		strings.NewReader(form.Encode()),
	)
	disableReq.AddCookie(cookie)
	disableReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	disabledResponse := httptest.NewRecorder()
	srv.ServeHTTP(disabledResponse, disableReq)
	if disabledResponse.Code != http.StatusOK {
		t.Fatalf("edge route status=%d", disabledResponse.Code)
	}
	var retainedMode string
	if err := store.Pool.QueryRow(ctx, "SELECT edge_widget_start_mode FROM backend.edge_property_settings WHERE property_id=$1", property.ID).
		Scan(&retainedMode); err != nil ||
		retainedMode != "load" {
		t.Fatalf("disabling edge changed widget settings: %q, err=%v", retainedMode, err)
	}
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))), nil)
	getReq.AddCookie(cookie)
	getReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	getReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	disabledModel, _, err := server.getOrgPropertySettings(httptest.NewRecorder(), getReq)
	if err != nil || disabledModel.EdgeWidgetStartMode != "click" {
		t.Fatalf("disabled edge defaults: %+v, err=%v", disabledModel, err)
	}

	noDomain, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, ""), org)
	if err != nil {
		t.Fatal(err)
	}
	form.Set(common.ParamName, noDomain.Name)
	form.Set(common.ParamEdgeTokenValidityInterval, "4")
	form.Set(common.ParamEdgeWidgetStartMode, "click")
	noDomainReq := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(noDomain.ID))),
		strings.NewReader(form.Encode()),
	)
	noDomainReq.AddCookie(cookie)
	noDomainReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	noDomainReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	noDomainReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(noDomain.ID)))
	view, err = server.putPropertyEdgeSettings(httptest.NewRecorder(), noDomainReq)
	if err != nil {
		t.Fatal(err)
	}
	if view.Model.(*edgePropertySettingsRenderContext).ErrorMessage == "" {
		t.Fatal("portal accepted edge lifetime for a property without a domain")
	}
	stored, err = store.Impl().RetrieveOrgProperty(ctx, org, noDomain.ID)
	if err != nil || stored.EdgeTokenValidityInterval != 0 {
		t.Fatalf("domainless property edge lifetime = %v, err = %v", stored, err)
	}
}

func TestPutPropertyPreservesConcurrentEdgeDisable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "edge-concurrent.example.com"), org)
	if err != nil {
		t.Fatal(err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	previous := server.EdgeTokens
	edgeConfig := config.NewBaseConfig(config.NewEnvConfig(func(string) string { return "" }))
	edgeConfig.Add(config.NewStaticValue(common.EdgeTokenSigningPrivateKeyKey, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))))
	edgeConfig.Add(config.NewStaticValue(common.EdgeTokenSigningPublicKeyKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))))
	server.EdgeTokens = api.NewEdgeTokenSigner("", edgeConfig)
	if err := server.EdgeTokens.Update(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { server.EdgeTokens = previous }()

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	enableForm := url.Values{}
	enableForm.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	enableForm.Set(common.ParamEdgeTokenValidityInterval, "4")
	enableForm.Set(common.ParamEdgeWidgetStartMode, "load")
	enableReq := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/org/%s/property/%s/edge", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))),
		strings.NewReader(enableForm.Encode()),
	)
	enableReq.AddCookie(cookie)
	enableReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	enableReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	enableReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	enableView, err := server.putPropertyEdgeSettings(httptest.NewRecorder(), enableReq)
	if err != nil {
		t.Fatal(err)
	}
	if enableView.Model.(*edgePropertySettingsRenderContext).SuccessMessage == "" {
		t.Fatalf("enabling edge protection failed: %+v", enableView)
	}
	enabled, err := store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil || enabled.EdgeTokenValidityInterval != time.Hour {
		t.Fatalf("edge protection not enabled with 1h validity: %+v, err=%v", enabled, err)
	}

	// Commit a concurrent authorized change that disables edge protection
	// directly against the DB, leaving the property cache holding the stale
	// 1-hour value that putProperty read at the start of its request. This
	// reproduces the race window between putProperty's initial read and its
	// UPDATE, without relying on goroutine timing.
	if _, err := store.Pool.Exec(ctx, "UPDATE backend.properties SET edge_token_validity_interval = INTERVAL '0 seconds', updated_at = NOW() WHERE id = $1", property.ID); err != nil {
		t.Fatal(err)
	}

	basicForm := url.Values{}
	basicForm.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	basicForm.Set(common.ParamName, property.Name+" renamed")
	basicForm.Set(common.ParamDifficulty, strconv.Itoa(int(property.Level.Int16)))
	basicForm.Set(common.ParamGrowth, strconv.Itoa(growthLevelToIndex(property.Growth)))
	basicForm.Set(common.ParamValidityInterval, strconv.Itoa(puzzle.ValidityIntervalToIndex(property.ValidityInterval)))
	basicReq := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/org/%s/property/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(property.ID))),
		strings.NewReader(basicForm.Encode()),
	)
	basicReq.AddCookie(cookie)
	basicReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	basicReq.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	basicReq.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	view, err := server.putProperty(httptest.NewRecorder(), basicReq)
	if err != nil {
		t.Fatal(err)
	}
	if model := view.Model.(*propertySettingsRenderContext); model.ErrorMessage != "" {
		t.Fatalf("basic settings save returned error: %q", model.ErrorMessage)
	}

	stored, err := store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != property.Name+" renamed" {
		t.Errorf("basic settings save did not rename: got %q, want %q", stored.Name, property.Name+" renamed")
	}
	if stored.EdgeTokenValidityInterval != 0 {
		t.Fatalf("basic settings save reverted concurrent edge disable: edge validity = %v, want 0", stored.EdgeTokenValidityInterval)
	}
}

func TestPortalPropertyUpdatesChallenge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "portal-challenge.com"), org)
	if err != nil {
		t.Fatal(err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))
	propertyID := server.IDHasher.Encrypt(int(property.ID))
	platformCtx := server.PlatformCtx.(*PlatformRenderContext)
	previousBudget := server.Argon2IDMemoryBudget
	previousFlags := server.FeatureFlags
	featuresEnabled := true
	server.FeatureFlags = featureFlagsFunc(func(_ context.Context, feature string, userID, checkedOrgID *int32) bool {
		if feature != common.FeatureArgon2ID || userID == nil || *userID != user.ID || checkedOrgID == nil || *checkedOrgID != org.ID {
			t.Error("feature check did not receive the authenticated user and property organization")
		}
		return featuresEnabled
	})
	setBudget := func(value string) {
		budget := config.NewStaticValue(common.Argon2IDMemoryBudgetKey, value)
		server.Argon2IDMemoryBudget = budget
		platformCtx.Argon2IDMemoryBudget = budget
	}
	setBudget("0")
	t.Cleanup(func() {
		server.FeatureFlags = previousFlags
		server.Argon2IDMemoryBudget = previousBudget
		platformCtx.Argon2IDMemoryBudget = previousBudget
	})

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, property.Name)
	form.Set(common.ParamDifficulty, strconv.Itoa(int(property.Level.Int16)))
	form.Set(common.ParamGrowth, "2")
	form.Set(common.ParamValidityInterval, "4")
	form.Set(common.ParamChallenge, string(dbgen.ChallengeTypeArgon2ID))

	sendUpdate := func() *httptest.ResponseRecorder {
		updateReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/org/%s/property/%s/edit", orgID, propertyID), strings.NewReader(form.Encode()))
		updateReq.AddCookie(cookie)
		updateReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
		updateResponse := httptest.NewRecorder()
		srv.ServeHTTP(updateResponse, updateReq)
		return updateResponse
	}
	updateResponse := sendUpdate()
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d", updateResponse.Code, http.StatusOK)
	}

	updatedProperty, err := server.Store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedProperty.Challenge != dbgen.ChallengeTypeBlake2b {
		t.Fatalf("challenge while disabled = %q, want %q", updatedProperty.Challenge, dbgen.ChallengeTypeBlake2b)
	}

	setBudget("256")
	featuresEnabled = false
	updateResponse = sendUpdate()
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("feature-disabled update status = %d, want %d", updateResponse.Code, http.StatusOK)
	}
	updatedProperty, err = server.Store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedProperty.Challenge != dbgen.ChallengeTypeBlake2b {
		t.Fatalf("challenge while feature disabled = %q, want Blake2b", updatedProperty.Challenge)
	}

	featuresEnabled = true
	updateResponse = sendUpdate()
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("enabled update status = %d, want %d", updateResponse.Code, http.StatusOK)
	}

	updatedProperty, err = server.Store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedProperty.Challenge != dbgen.ChallengeTypeArgon2ID {
		t.Fatalf("updated property challenge = %q, want %q", updatedProperty.Challenge, dbgen.ChallengeTypeArgon2ID)
	}

	var persistedChallenge string
	if err := store.Pool.QueryRow(ctx, "SELECT challenge FROM backend.properties WHERE id = $1", property.ID).Scan(&persistedChallenge); err != nil {
		t.Fatal(err)
	}
	if dbgen.ChallengeType(persistedChallenge) != dbgen.ChallengeTypeArgon2ID {
		t.Fatalf("persisted challenge = %q, want %q", persistedChallenge, dbgen.ChallengeTypeArgon2ID)
	}

	setBudget("0")
	form.Del(common.ParamChallenge)
	form.Set(common.ParamGrowth, "3")
	updateResponse = sendUpdate()
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update without challenge status = %d, want %d", updateResponse.Code, http.StatusOK)
	}
	updatedProperty, err = server.Store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedProperty.Challenge != dbgen.ChallengeTypeArgon2ID || updatedProperty.Growth != dbgen.DifficultyGrowthFast {
		t.Fatalf("updated property = challenge %q, growth %q; want preserved Argon2id and fast growth", updatedProperty.Challenge, updatedProperty.Growth)
	}

	setBudget("256")
	featuresEnabled = false
	form.Set(common.ParamGrowth, "1")
	updateResponse = sendUpdate()
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("feature-disabled unrelated update status = %d, want %d", updateResponse.Code, http.StatusOK)
	}
	updatedProperty, err = server.Store.Impl().RetrieveOrgProperty(ctx, org, property.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedProperty.Challenge != dbgen.ChallengeTypeArgon2ID || updatedProperty.Growth != dbgen.DifficultyGrowthSlow {
		t.Fatal("unrelated update should preserve Argon2id when its feature flag is disabled")
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

func TestDeletePropertyAttachedToFormFailsGracefully(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, property, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "delete-attached.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit/delete-attached"),
		org)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}
	_ = form

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

	if w.Code != http.StatusConflict {
		t.Errorf("Expected 409 Conflict for attached property delete, got %d", w.Code)
	}

	properties, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(properties) != 1 || properties[0].ID != property.ID {
		t.Error("Property should not have been deleted")
	}
}

func TestMovePropertyAttachedToFormFailsGracefully(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	form, property, _, err := store.Impl().CreateNewForm(ctx,
		db_tests.CreateNewPropertyParams(user.ID, "move-attached.example.com"),
		db_tests.CreateNewFormParams(user.ID, "https://example.com/submit/move-attached"),
		org1)
	if err != nil {
		t.Fatalf("Failed to create form: %v", err)
	}
	_ = form

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

	formData := url.Values{}
	formData.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	formData.Set(common.ParamOrg, server.IDHasher.Encrypt(int(org2.ID)))

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/%s/move", server.IDHasher.Encrypt(int(org1.ID)), server.IDHasher.Encrypt(int(property.ID))), strings.NewReader(formData.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("Expected 409 Conflict for attached property move, got %d", w.Code)
	}

	properties, _, err := store.Impl().RetrieveOrgPropertiesByDateAscending(ctx, org1, 0, db.MaxOrgPropertiesPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(properties) != 1 || properties[0].ID != property.ID {
		t.Error("Property should not have been moved")
	}
}
