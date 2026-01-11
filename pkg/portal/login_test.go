package portal

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
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

	if _, err := portal_tests.TwoFactorCodeFromResponse(ctx, resp, server.Sessions); err != nil {
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

	// Verify session exists before logout
	sessionID, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		t.Fatalf("failed to unescape session ID: %v", err)
	}

	// Wait until the session is persisted to cache (background job)
	for attempt := 0; attempt < 6; attempt++ {
		time.Sleep(250 * time.Millisecond)

		_, err = server.Sessions.Store.Read(ctx, sessionID, true /*skip cache*/)
		if err == nil {
			break
		}
	}

	if err != nil {
		t.Fatalf("session should exist in cache before logout: %v", err)
	}

	// Perform logout
	logoutReq := httptest.NewRequest("GET", "/"+common.LogoutEndpoint, nil)
	logoutReq.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, logoutReq)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected logout response code: got %v want %v", resp.StatusCode, http.StatusSeeOther)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatalf("failed to get redirect location: %v", err)
	}

	if location.Path != "/"+common.LoginEndpoint {
		t.Errorf("Unexpected redirect location: got %v want %v", location.Path, "/"+common.LoginEndpoint)
	}

	_, err = server.Sessions.Store.Read(ctx, sessionID, true /*skip cache*/)
	if err != session.ErrSessionMissing {
		t.Errorf("session should be destroyed after logout: got error %v, want %v", err, session.ErrSessionMissing)
	}
}
