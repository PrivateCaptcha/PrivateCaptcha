package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type failingLogoutStore struct {
	session.Store
	err error
}

func (s *failingLogoutStore) Destroy(context.Context, string) (session.SessionRevocationResult, error) {
	return session.SessionRevocationResult{}, s.err
}

func parseCsrfToken(body string) (string, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return "", err
	}

	var csrfToken string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "hx-headers" {
					var headers map[string]string
					if json.Unmarshal([]byte(a.Val), &headers) == nil {
						csrfToken = headers[common.HeaderCSRFToken]
					}
				}
			}
		}

		if len(csrfToken) == 0 && n.Type == html.ElementNode && n.Data == "input" {
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
	responseCookieForTest(t, resp, server.Sessions.CookieName)
	signInCodeForTest(t, user.Email)
}

func TestPostLoginDoesNotRebindPendingSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	firstUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"First", testPlan)
	if err != nil {
		t.Fatal(err)
	}
	secondUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"Second", testPlan)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	firstResponse := loginSuite(srv, firstUser.Email, server.XSRF.Token(""))
	firstCookie := responseCookieForTest(t, firstResponse, server.Sessions.CookieName)
	firstCode := signInCodeForTest(t, firstUser.Email)

	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, secondUser.Email)
	form.Add(common.ParamPortalSolution, "captchaSolution")
	req := httptest.NewRequest(http.MethodPost, "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.AddCookie(firstCookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	secondResponse := w.Result()
	if secondResponse.StatusCode != http.StatusOK {
		t.Fatalf("second login status = %d, want 200", secondResponse.StatusCode)
	}
	secondCookie := responseCookieForTest(t, secondResponse, server.Sessions.CookieName)
	if secondCookie.Value == firstCookie.Value {
		t.Fatal("second login reused the first pending SID")
	}

	response := twoFactorSuite(srv, firstUser.Email, server.XSRF.Token(firstCookie.Value), firstCode, firstCookie)
	location, _ := response.Location()
	if response.StatusCode != http.StatusSeeOther || location.Path != "/" {
		t.Fatalf("first challenge response = (%d, %v), want successful root redirect", response.StatusCode, location)
	}
	rotatedCookie := responseCookieForTest(t, response, server.Sessions.CookieName)
	successor, err := dbgen.New(store.Pool).GetSessionByID(ctx, rotatedCookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if !successor.UserID.Valid || successor.UserID.Int32 != firstUser.ID {
		t.Fatalf("successor user = %v, want %d", successor.UserID, firstUser.ID)
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
	user, _, err := db_tests.CreateNewAccountForTest(t.Context(), store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
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
	form.Add(common.ParamEmail, user.Email)
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

	if len(rr.Result().Cookies()) != 0 {
		t.Fatal("missing CAPTCHA created a session cookie")
	}
	if _, ok := testMailer.TwoFactorCode(user.Email); ok {
		t.Fatal("missing CAPTCHA sent a sign-in code")
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
	remote, _ := newChallengeReplica(t)
	remoteSession, err := remote.Sessions.Store.Read(ctx, sessionID, true /*skip cache*/)
	if err != nil {
		t.Fatal(err)
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
	expiredCookie := responseCookieForTest(t, resp, server.Sessions.CookieName)
	if expiredCookie.MaxAge != -1 || expiredCookie.Value != "" {
		t.Fatal("successful logout did not expire the session cookie")
	}

	_, err = server.Sessions.Store.Read(ctx, sessionID, true /*skip cache*/)
	if err != session.ErrSessionMissing {
		t.Errorf("session should be destroyed after logout: got error %v, want %v", err, session.ErrSessionMissing)
	}
	if _, err := remote.Sessions.Store.Read(ctx, sessionID, false /*skip cache*/); err != nil {
		t.Fatalf("remote session inside validation lease = %v", err)
	}
	remoteSession.Data().MarkValidated(time.Now().Add(-11 * time.Minute))
	if _, err := remote.Sessions.Store.Read(ctx, sessionID, false /*skip cache*/); err != session.ErrSessionMissing {
		t.Fatalf("remote session after validation lease = %v, want %v", err, session.ErrSessionMissing)
	}

	repeatedReq := httptest.NewRequest(http.MethodGet, "/"+common.LogoutEndpoint, nil)
	repeatedReq.AddCookie(cookie)
	repeatedW := httptest.NewRecorder()
	srv.ServeHTTP(repeatedW, repeatedReq)
	if repeatedW.Code != http.StatusSeeOther {
		t.Fatalf("repeated logout status = %d, want %d", repeatedW.Code, http.StatusSeeOther)
	}
}

func TestLogoutCookieHandling(t *testing.T) {
	t.Run("RevocationFailure", func(t *testing.T) {
		for name, invoke := range map[string]func(*Server, http.ResponseWriter, *http.Request){
			"DirectLogout": func(s *Server, w http.ResponseWriter, r *http.Request) { s.logout(w, r) },
			"InvalidSessionCleanup": func(s *Server, w http.ResponseWriter, r *http.Request) {
				s.handlePortalError(0, ErrInvalidSession, w, r)
			},
		} {
			t.Run(name, func(t *testing.T) {
				srv := &Server{Sessions: &session.Manager{
					CookieName: "pcsid",
					Store:      &failingLogoutStore{err: errors.New("database unavailable")},
					Path:       "/",
				}}
				req := httptest.NewRequest(http.MethodGet, "/"+common.LogoutEndpoint, nil)
				req.AddCookie(&http.Cookie{Name: "pcsid", Value: "logout-failure-sid"})
				w := httptest.NewRecorder()

				invoke(srv, w, req)

				if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
					t.Fatalf("failed logout cleanup = (status %d, cookies %d), want (503, 0)", w.Code, len(w.Result().Cookies()))
				}
			})
		}
	})

	t.Run("InvalidCookie", func(t *testing.T) {
		for name, value := range map[string]string{
			"Empty":       "",
			"Malformed":   "%ZZ",
			"NUL":         "%00",
			"InvalidUTF8": "%FF",
			"Oversized":   strings.Repeat("a", 129),
		} {
			t.Run(name, func(t *testing.T) {
				srv := &Server{Sessions: &session.Manager{
					CookieName: "pcsid",
					Store:      &failingLogoutStore{err: errors.New("store must not be called")},
					Path:       "/",
				}}
				req := httptest.NewRequest(http.MethodGet, "/"+common.LogoutEndpoint, nil)
				req.AddCookie(&http.Cookie{Name: "pcsid", Value: value})
				w := httptest.NewRecorder()

				srv.logout(w, req)

				if w.Code != http.StatusSeeOther {
					t.Fatalf("invalid-cookie logout status = %d, want %d", w.Code, http.StatusSeeOther)
				}
				expired := responseCookieForTest(t, w.Result(), "pcsid")
				if expired.MaxAge != -1 || expired.Value != "" {
					t.Fatal("invalid session cookie was not expired")
				}
			})
		}
	})
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
	user, _, err := db_tests.CreateNewAccountForTest(t.Context(), store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
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
	form.Add(common.ParamEmail, user.Email)
	form.Add(common.ParamPortalSolution, "invalid-captcha-solution")

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", rr.Code)
	}

	if len(rr.Result().Cookies()) != 0 {
		t.Fatal("invalid CAPTCHA created a session cookie")
	}
	if _, ok := testMailer.TwoFactorCode(user.Email); ok {
		t.Fatal("invalid CAPTCHA sent a sign-in code")
	}
}
