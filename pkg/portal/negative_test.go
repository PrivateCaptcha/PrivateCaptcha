package portal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func TestOrgEndpointsInvalidPathArg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
	}{
		{"GetOrgDashboardInvalidOrg", "GET", "/org/invalid-id/tab/dashboard", http.StatusSeeOther},
		{"GetOrgMembersInvalidOrg", "GET", "/org/invalid-id/tab/members", http.StatusSeeOther},
		{"GetOrgSettingsInvalidOrg", "GET", "/org/invalid-id/tab/settings", http.StatusSeeOther},
		{"GetOrgAuditLogsInvalidOrg", "GET", "/org/invalid-id/tab/events", http.StatusSeeOther},
		{"GetOrgPropertiesInvalidOrg", "GET", "/org/invalid-id/properties", http.StatusSeeOther},
		{"GetNewPropertyInvalidOrg", "GET", "/org/invalid-id/property/new", http.StatusSeeOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	orgID := server.IDHasher.Encrypt(int(org.ID))

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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestOrgEndpointsWrongOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	_, org1, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
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

	org1ID := server.IDHasher.Encrypt(int(org1.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user2.ID)))

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		useCSRF  bool
		formBody url.Values
	}{
		{"GetOrgMembersWrongOwner", "GET", fmt.Sprintf("/org/%s/tab/members", org1ID), http.StatusSeeOther, false, nil},
		{"GetOrgSettingsWrongOwner", "GET", fmt.Sprintf("/org/%s/tab/settings", org1ID), http.StatusSeeOther, false, nil},
		{"GetOrgAuditLogsWrongOwner", "GET", fmt.Sprintf("/org/%s/tab/events", org1ID), http.StatusSeeOther, false, nil},
		{"PutOrgWrongOwner", "PUT", fmt.Sprintf("/org/%s/edit", org1ID), http.StatusSeeOther, true, url.Values{common.ParamName: {"NewName"}}},
		{"DeleteOrgWrongOwner", "DELETE", fmt.Sprintf("/org/%s/delete", org1ID), http.StatusSeeOther, true, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.formBody != nil {
				tc.formBody.Set(common.ParamCSRFToken, csrfToken)
				body = strings.NewReader(tc.formBody.Encode())
			} else {
				body = strings.NewReader("")
			}

			req := httptest.NewRequest(tc.method, tc.path, body)
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user2.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
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

func TestOrgEndpointsMissingSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewBareAccount(ctx, store, t.Name())
	if err != nil {
		t.Fatalf("Failed to create account without subscription: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	t.Run("PostNewOrgMissingSubscription", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "NewOrgNoSubscription")

		req := httptest.NewRequest("POST", "/org/new", strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK with error message, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "subscription") {
			t.Error("Expected response to mention subscription requirement")
		}
	})
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
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
			t.Errorf("Expected status OK with error message, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "subscription") {
			t.Error("Expected response to mention subscription requirement")
		}
	})
}

func TestAPIKeyEndpointsInvalidPathArg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	ctx := t.Context()
	owner, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	key, _, err := store.Impl().CreateAPIKey(ctx, owner, db_tests.CreateNewPuzzleAPIKeyParams("OwnerKey", time.Now(), 24*time.Hour, 10.0))
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
		{"RotateAPIKeyWrongOwner", "POST", fmt.Sprintf("/apikeys/%s", keyID), http.StatusInternalServerError},
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

	ctx := t.Context()
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

func TestOrgMemberEndpointsInvalidForm(t *testing.T) {
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

	orgID := server.IDHasher.Encrypt(int(org.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	tests := []struct {
		name     string
		formBody url.Values
		checkErr string
	}{
		{
			name: "InviteSelfToOrg",
			formBody: url.Values{
				common.ParamEmail: {user.Email},
			},
			checkErr: "already a member",
		},
		{
			name: "InviteInvalidEmail",
			formBody: url.Values{
				common.ParamEmail: {"invalid-email"},
			},
			checkErr: "not valid",
		},
		{
			name: "InviteNonExistentUser",
			formBody: url.Values{
				common.ParamEmail: {"nonexistent@example.com"},
			},
			checkErr: "Cannot find user",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.formBody.Set(common.ParamCSRFToken, csrfToken)

			req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/members", orgID), strings.NewReader(tc.formBody.Encode()))
			req.AddCookie(cookie)
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			body := w.Body.String()
			if !strings.Contains(body, tc.checkErr) {
				t.Errorf("%s: expected response to contain '%s'", tc.name, tc.checkErr)
			}
		})
	}
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
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

func TestOrgEndpointsInvalidFormArgs(t *testing.T) {
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

	orgID := server.IDHasher.Encrypt(int(org.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	t.Run("PutOrgEmptyName", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "")

		req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/edit", orgID), strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d", w.Code)
		}
	})

	t.Run("PutOrgShortName", func(t *testing.T) {
		form := url.Values{}
		form.Set(common.ParamCSRFToken, csrfToken)
		form.Set(common.ParamName, "ab")

		req := httptest.NewRequest("PUT", fmt.Sprintf("/org/%s/edit", orgID), strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d", w.Code)
		}
	})
}

func TestDeleteOrgMemberInvalidPathArg(t *testing.T) {
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

	orgID := server.IDHasher.Encrypt(int(org.ID))
	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/org/%s/members/invalid-user-id", orgID), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status, got %d", w.Code)
	}
}

func TestJoinOrgInvalidPathArg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	req := httptest.NewRequest("PUT", "/org/invalid-org-id/members", nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status, got %d", w.Code)
	}
}

func TestLeaveOrgInvalidPathArg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	req := httptest.NewRequest("DELETE", "/org/invalid-org-id/members", nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status, got %d", w.Code)
	}
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
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

func TestAuditLogEndpointsInvalidParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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

	tests := []struct {
		name     string
		path     string
		wantCode int
	}{
		{"GetAuditLogsInvalidDays", "/auditlogs/events?days=999", http.StatusOK},
		{"GetAuditLogsInvalidPage", "/auditlogs/events?page=-1", http.StatusOK},
		{"ExportAuditLogsInvalidDays", "/auditlogs/export?days=abc", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("%s: got status %d, want %d", tc.name, w.Code, tc.wantCode)
			}
		})
	}
}

func TestSettingsTabInvalidTab(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
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
