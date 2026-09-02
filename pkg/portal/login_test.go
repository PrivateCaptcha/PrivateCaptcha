package portal

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func parseCsrfToken(body string) (string, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return "", err
	}

	var csrfToken string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			isCsrfElement := false
			token := ""

			for _, a := range n.Attr {
				if a.Key == "name" && a.Val == common.ParamCSRFToken {
					isCsrfElement = true
				}

				if a.Key == "type" && a.Val == "hidden" {
					for _, a := range n.Attr {
						if a.Key == "value" {
							token = a.Val
						}
					}
				}
			}

			if isCsrfElement && (len(token) > 0) && (len(csrfToken) == 0) {
				csrfToken = token
			}
		}

		if len(csrfToken) == 0 {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}
	}
	f(doc)

	return csrfToken, nil
}

func TestGetLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)

	rr := httptest.NewRecorder()

	server.Handler(server.getLogin).ServeHTTP(rr, req)

	// check if the status code is 200
	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}

	token, err := parseCsrfToken(rr.Body.String())
	if (err != nil) || (token == "") {
		t.Errorf("failed to parse csrf token: %v", err)
	}

	if !server.XSRF.VerifyToken(token, "") {
		t.Error("Failed to verify token in Login form")
	}
}

func TestGetLoginMaintenance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)

	server.maintenanceMode.Store(true)
	defer server.maintenanceMode.Store(false)

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v", resp.StatusCode, http.StatusSeeOther)
	}
}

func TestPostLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	// Get the CSRF token
	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)
	rr := httptest.NewRecorder()
	server.Handler(server.getLogin).ServeHTTP(rr, req)
	csrfToken, err := parseCsrfToken(rr.Body.String())
	if err != nil {
		t.Fatalf("failed to parse CSRF token: %v", err)
	}

	// Prepare the form data
	form := url.Values{}
	form.Add(common.ParamCSRFToken, csrfToken)
	form.Add(common.ParamEmail, user.Email)
	form.Add(common.ParamPortalSolution, "captcha solution")

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)
	resp := rr.Result()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected post login code: %v", resp.StatusCode)
	}

	if _, err := portal_tests.TwoFactorCodeFromEmail(user.Email); err != nil {
		t.Error(err)
	}
}

func TestPostLoginEmptyEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Get the CSRF token
	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)
	rr := httptest.NewRecorder()
	server.Handler(server.getLogin).ServeHTTP(rr, req)
	csrfToken, err := parseCsrfToken(rr.Body.String())
	if err != nil {
		t.Fatalf("failed to parse CSRF token: %v", err)
	}

	// Prepare the form data with empty email
	form := url.Values{}
	form.Add(common.ParamCSRFToken, csrfToken)
	form.Add(common.ParamEmail, "")
	form.Add(common.ParamPortalSolution, "captcha solution")

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)

	// Empty email should fail validation
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", rr.Code)
	}

	// Response body should contain an error message
	body := rr.Body.String()
	if !strings.Contains(body, "not valid") && !strings.Contains(body, "error") {
		t.Error("Expected error message for invalid email")
	}
}

func TestPostLoginMalformedEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Get the CSRF token
	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)
	rr := httptest.NewRecorder()
	server.Handler(server.getLogin).ServeHTTP(rr, req)
	csrfToken, err := parseCsrfToken(rr.Body.String())
	if err != nil {
		t.Fatalf("failed to parse CSRF token: %v", err)
	}

	// Prepare the form data with malformed email
	form := url.Values{}
	form.Add(common.ParamCSRFToken, csrfToken)
	form.Add(common.ParamEmail, "not-an-email")
	form.Add(common.ParamPortalSolution, "captcha solution")

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)

	// Malformed email should fail validation
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "not valid") {
		t.Error("Expected error message for malformed email")
	}
}

func TestPostLoginNonexistentUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Get the CSRF token
	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)
	rr := httptest.NewRecorder()
	server.Handler(server.getLogin).ServeHTTP(rr, req)
	csrfToken, err := parseCsrfToken(rr.Body.String())
	if err != nil {
		t.Fatalf("failed to parse CSRF token: %v", err)
	}

	// Prepare the form data with email that doesn't exist
	form := url.Values{}
	form.Add(common.ParamCSRFToken, csrfToken)
	form.Add(common.ParamEmail, "nonexistent-user-42@example.com")
	form.Add(common.ParamPortalSolution, "captcha solution")

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)

	// Nonexistent user should fail
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "does not exist") {
		t.Error("Expected error message for nonexistent user")
	}
}

func TestPostLoginMissingCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Get the CSRF token
	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)
	rr := httptest.NewRecorder()
	server.Handler(server.getLogin).ServeHTTP(rr, req)
	csrfToken, err := parseCsrfToken(rr.Body.String())
	if err != nil {
		t.Fatalf("failed to parse CSRF token: %v", err)
	}

	// Prepare the form data WITHOUT captcha solution
	form := url.Values{}
	form.Add(common.ParamCSRFToken, csrfToken)
	form.Add(common.ParamEmail, "test@example.com")
	// No captcha solution

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)

	// Missing captcha should fail
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "captcha") {
		t.Error("Expected error message for missing captcha")
	}
}

func TestLogout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	resolvedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	resolvedReq.AddCookie(cookie)
	if _, err := server.Sessions.Get(resolvedReq); err != nil {
		t.Fatalf("session should resolve before logout: %v", err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	portalReq := httptest.NewRequest(http.MethodGet, "/", nil)
	portalReq.AddCookie(cookie)
	portalW := httptest.NewRecorder()
	srv.ServeHTTP(portalW, portalReq)
	if portalW.Code != http.StatusOK {
		t.Fatalf("portal status = %v, want OK", portalW.Code)
	}
	assertLogoutButtons(t, portalW.Body, user.ID)

	logoutGet := httptest.NewRequest(http.MethodGet, "/"+common.LogoutEndpoint, nil)
	logoutGet.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, logoutGet)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET logout status = %v, want %v", w.Code, http.StatusMethodNotAllowed)
	}
	if _, err := server.Sessions.Get(logoutGet); err != nil {
		t.Fatalf("GET logout revoked session: %v", err)
	}

	missingCSRFReq := httptest.NewRequest(http.MethodPost, "/"+common.LogoutEndpoint, nil)
	missingCSRFReq.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, missingCSRFReq)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("logout without CSRF status = %v, want redirect", w.Code)
	}
	if _, err := server.Sessions.Get(missingCSRFReq); err != nil {
		t.Fatalf("logout without CSRF revoked session: %v", err)
	}

	invalidForm := url.Values{}
	invalidForm.Add(common.ParamCSRFToken, "invalid")
	invalidCSRFReq := httptest.NewRequest(http.MethodPost, "/"+common.LogoutEndpoint, bytes.NewBufferString(invalidForm.Encode()))
	invalidCSRFReq.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	invalidCSRFReq.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, invalidCSRFReq)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("logout with invalid CSRF status = %v, want redirect", w.Code)
	}
	if _, err := server.Sessions.Get(invalidCSRFReq); err != nil {
		t.Fatalf("logout with invalid CSRF revoked session: %v", err)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/"+common.LogoutEndpoint, nil)
	logoutReq.Header.Set(common.HeaderCSRFToken, csrfToken)
	logoutReq.Header.Set(common.HeaderHtmxRequest, "true")
	logoutReq.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, logoutReq)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected logout response code: got %v want %v", resp.StatusCode, http.StatusOK)
	}
	if redirect := resp.Header.Get("HX-Redirect"); redirect != "/"+common.LoginEndpoint {
		t.Errorf("Unexpected HTMX redirect: got %v want %v", redirect, "/"+common.LoginEndpoint)
	}

	_, err = server.Sessions.Get(logoutReq)
	if err != session.ErrSessionMissing {
		t.Errorf("session should be revoked after logout: got error %v, want %v", err, session.ErrSessionMissing)
	}
}

func assertLogoutButtons(t *testing.T, body *bytes.Buffer, userID int32) {
	t.Helper()
	document := portal_tests.ParseHTML(t, body)
	buttons := document.Find(`button[type="button"][hx-post="/logout"][hx-swap="none"]`)
	if buttons.Length() != 2 {
		t.Fatalf("logout button count = %d, want 2", buttons.Length())
	}
	if controls := document.Find(`a[href="/logout"], form[action="/logout"]`); controls.Length() != 0 {
		t.Fatalf("non-HTMX logout control count = %d, want 0", controls.Length())
	}
	headersJSON, ok := document.Find("body").Attr("hx-headers")
	if !ok {
		t.Fatal("page does not provide inherited HTMX headers")
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		t.Fatalf("invalid HTMX headers: %v", err)
	}
	if !server.XSRF.VerifyToken(headers[common.HeaderCSRFToken], strconv.Itoa(int(userID))) {
		t.Fatal("page contains an invalid inherited CSRF token")
	}
}

func TestPortalPropertyOwnerSourceOwnerID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create a property
	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "owner-source-test.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Create the owner source
	ownerSource := &portalPropertyOwnerSource{
		Store:   store,
		Sitekey: db.UUIDToSiteKey(property.ExternalID),
	}

	// Test OwnerID
	ownerID, orgID, err := ownerSource.OwnerID(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if ownerID != user.ID {
		t.Errorf("Expected ownerID %d, got %d", user.ID, ownerID)
	}

	if orgID == nil {
		t.Fatal("Expected orgID to be non-nil")
	}

	if *orgID != org.ID {
		t.Errorf("Expected orgID %d, got %d", org.ID, *orgID)
	}
}

func TestPortalPropertyOwnerSourceOwnerIDNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create the owner source with non-existent sitekey
	ownerSource := &portalPropertyOwnerSource{
		Store:   store,
		Sitekey: "non-existent-sitekey-123456",
	}

	// Test OwnerID should fail
	_, _, err := ownerSource.OwnerID(ctx, time.Now().UTC())
	if err == nil {
		t.Error("Expected error for non-existent sitekey")
	}

	if err != errPortalPropertyNotFound {
		t.Errorf("Expected errPortalPropertyNotFound, got: %v", err)
	}
}

func TestPostLoginParseFormFail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Create a request with invalid URL-encoded form data that will fail ParseForm
	// Using %ZZ which is an invalid percent-encoding
	req := httptest.NewRequest("POST", "/"+common.LoginEndpoint, strings.NewReader("email=%ZZ"))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// When ParseForm fails, server redirects to error endpoint
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303), got %d", w.Code)
	}
}

func TestPostLoginDisabledUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	// Disable the user
	if err := db_tests.DisableUserForTest(ctx, store, user.ID); err != nil {
		t.Fatalf("failed to disable user: %v", err)
	}

	// Clear user cache to ensure disabled status is fetched from DB
	cache.Delete(ctx, db.UserCacheKey(user.ID))

	// Get the CSRF token
	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)
	rr := httptest.NewRecorder()
	server.Handler(server.getLogin).ServeHTTP(rr, req)
	csrfToken, err := parseCsrfToken(rr.Body.String())
	if err != nil {
		t.Fatalf("failed to parse CSRF token: %v", err)
	}

	// Prepare the form data
	form := url.Values{}
	form.Add(common.ParamCSRFToken, csrfToken)
	form.Add(common.ParamEmail, user.Email)
	form.Add(common.ParamPortalSolution, "captcha solution")

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)

	// Disabled user should see error
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "disabled") {
		t.Errorf("Expected error message about disabled account, got: %s", body)
	}
}

func TestPostLoginInvalidCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Temporarily make the puzzle engine return a failed verify result
	originalResult := server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result
	server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result = &puzzle.VerifyResult{Error: puzzle.InvalidSolutionError}
	defer func() {
		server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result = originalResult
	}()

	// Get the CSRF token
	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)
	rr := httptest.NewRecorder()
	server.Handler(server.getLogin).ServeHTTP(rr, req)
	csrfToken, err := parseCsrfToken(rr.Body.String())
	if err != nil {
		t.Fatalf("failed to parse CSRF token: %v", err)
	}

	// Prepare the form data with invalid captcha solution
	form := url.Values{}
	form.Add(common.ParamCSRFToken, csrfToken)
	form.Add(common.ParamEmail, "test@example.com")
	form.Add(common.ParamPortalSolution, "invalid-captcha-solution")

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, captchaVerificationFailed) {
		t.Errorf("Expected captcha verification failed error message, got: %s", body)
	}
}
