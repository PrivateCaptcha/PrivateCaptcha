package portal

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func TestPostTwoFactor(t *testing.T) {
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

	// request portal (any protected endpoint really)
	privReq := httptest.NewRequest("GET", "/", nil)
	privReq.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, privReq)

	if w.Code != http.StatusOK {
		t.Errorf("Unexpected portal response code: %v", w.Code)
	}
}

func loginSuite(srv *http.ServeMux, email, token string) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamPortalSolution, "captchaSolution")

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	return w.Result()
}

func twoFactorSuite(srv *http.ServeMux, email, token string, code int, cookie *http.Cookie) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamVerificationCode, strconv.Itoa(code))

	// now send the 2fa request
	req := httptest.NewRequest("POST", "/"+common.TwoFactorEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Result()
}

// technically it's close to TestPersistentSession, but it's more "end-to-end"
func TestPostTwoFactorOtherServer(t *testing.T) {
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

	resp := loginSuite(srv, user.Email, server.XSRF.Token(""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected login status code: %v", resp.StatusCode)
	}

	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find session cookie in response")
	}
	cookie := resp.Cookies()[idx]

	// wait until the session is persisted to DB
	for attempt := 0; attempt < 5; attempt++ {
		time.Sleep(400 * time.Millisecond)

		_, err = store.Impl().RetrieveFromCache(ctx, "session/"+cookie.Value)
		if err == nil {
			break
		}
	}

	if err != nil {
		t.Fatal(err)
	}

	if deleted := cache.Delete(ctx, db.SessionCacheKey(cookie.Value)); !deleted {
		t.Fatal("Didn't delete cached session")
	}

	code, err := portal_tests.TwoFactorCodeFromSession(ctx, cookie.Value, server.Sessions.Store)
	if err != nil {
		t.Fatal(err)
	}

	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), code, cookie)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	if location, _ := resp.Location(); location.String() != "/" {
		t.Errorf("unexpected redirect: %v", location)
	}
}

func TestPostTwoFactorRotatesSession(t *testing.T) {
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

	resp := loginSuite(srv, user.Email, server.XSRF.Token(""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected login status code: %v", resp.StatusCode)
	}

	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find session cookie in response")
	}
	oldCookie := resp.Cookies()[idx]

	code, err := portal_tests.TwoFactorCodeFromSession(ctx, oldCookie.Value, server.Sessions.Store)
	if err != nil {
		t.Fatal(err)
	}

	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), code, oldCookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	idx = slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find rotated session cookie in response")
	}
	newCookie := resp.Cookies()[idx]
	if newCookie.Value == oldCookie.Value {
		t.Fatal("session ID was not rotated after successful 2FA")
	}
	if newCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax, got %v", newCookie.SameSite)
	}

	oldReq := httptest.NewRequest("GET", "/", nil)
	oldReq.AddCookie(oldCookie)
	oldW := httptest.NewRecorder()
	srv.ServeHTTP(oldW, oldReq)
	if oldW.Code == http.StatusOK {
		t.Fatal("pre-2FA session cookie still authenticated after rotation")
	}

	newReq := httptest.NewRequest("GET", "/", nil)
	newReq.AddCookie(newCookie)
	newW := httptest.NewRecorder()
	srv.ServeHTTP(newW, newReq)
	if newW.Code != http.StatusOK {
		t.Fatalf("rotated session cookie was not authenticated: %v", newW.Code)
	}
}

func TestPostTwoFactorAttemptLimit(t *testing.T) {
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

	resp := loginSuite(srv, user.Email, server.XSRF.Token(""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected login status code: %v", resp.StatusCode)
	}

	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find session cookie in response")
	}
	cookie := resp.Cookies()[idx]

	code, err := portal_tests.TwoFactorCodeFromSession(ctx, cookie.Value, server.Sessions.Store)
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= maxFailedAttempts; attempt++ {
		resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), code+1, cookie)
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("failed to read attempt %d response: %v", attempt, readErr)
		}
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Code is not valid.") {
			t.Fatalf("unexpected response for failed attempt %d: status %v, body %s", attempt, resp.StatusCode, body)
		}
	}

	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), code, cookie)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read limited response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Too many failed attempts. Please request a new code.") {
		t.Fatalf("expected correct code to be rejected after five failures: status %v, body %s", resp.StatusCode, body)
	}

	resp = resend2faSuite(srv, user.Email, server.XSRF.Token(user.Email), cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected resend status code: %v", resp.StatusCode)
	}

	code, err = portal_tests.TwoFactorCodeFromSession(ctx, cookie.Value, server.Sessions.Store)
	if err != nil {
		t.Fatal(err)
	}

	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), code+1, cookie)
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read response after resend: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Code is not valid.") {
		t.Fatalf("expected resend to reset failed attempts: status %v, body %s", resp.StatusCode, body)
	}

	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), code, cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected post twofactor status code after resend: %v", resp.StatusCode)
	}

	idx = slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find rotated session cookie in response")
	}
	newSession, err := server.Sessions.Store.Read(ctx, resp.Cookies()[idx].Value, false /*skip cache*/)
	if err != nil {
		t.Fatal(err)
	}
	if newSession.Data().Has(session.KeyLoginAttempts) {
		t.Fatal("failed login attempts remain after successful verification")
	}
}

func resend2faSuite(srv *http.ServeMux, email, token string, cookie *http.Cookie) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)

	req := httptest.NewRequest("POST", "/"+common.ResendEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Result()
}

func TestResend2FA(t *testing.T) {
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

	// Login to get into 2FA verification state (this sends the original 2FA code)
	resp := loginSuite(srv, user.Email, server.XSRF.Token(""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected login status code: %v", resp.StatusCode)
	}

	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find session cookie in response")
	}
	cookie := resp.Cookies()[idx]

	originalCode, err := portal_tests.TwoFactorCodeFromSession(ctx, cookie.Value, server.Sessions.Store)
	if err != nil {
		t.Fatal(err)
	}

	// Try to use a wrong code first (but don't complete login)
	// This simulates a user who received the code but wants to resend
	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), 999999, cookie)
	// Using wrong code should fail
	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("Should not have succeeded with wrong code")
	}

	// Now call resend 2FA to get a new code - note: CSRF token uses email as key
	resp = resend2faSuite(srv, user.Email, server.XSRF.Token(user.Email), cookie)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected resend2fa status code: %v", resp.StatusCode)
	}

	// The code should have been reissued and should be different
	newCode, err := portal_tests.TwoFactorCodeFromSession(ctx, cookie.Value, server.Sessions.Store)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the new code is different from the original
	if newCode == originalCode {
		t.Errorf("New 2FA code should be different from original code (both were %d)", newCode)
	}

	// Verify we can use the new code to complete login
	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), newCode, cookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected post twofactor status code with reissued code: %v", resp.StatusCode)
	}
}

func TestResend2FAWithoutSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to resend 2FA without a valid session - should fail due to CSRF
	// (the CSRF middleware requires a session with email to verify the token)
	form := url.Values{}
	form.Add(common.ParamCSRFToken, "invalid-token")

	req := httptest.NewRequest("POST", "/"+common.ResendEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp := w.Result()

	// Should redirect to expired page due to CSRF failure
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status code: %v", resp.StatusCode)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatal(err)
	}

	// Without valid session, CSRF will fail and redirect to expired endpoint
	if location.Path != "/"+common.ExpiredEndpoint {
		t.Errorf("Expected redirect to expired, got: %v", location.Path)
	}
}

func TestResend2FAWithCompletedSession(t *testing.T) {
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

	// Complete the full authentication
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to resend 2FA with an already completed session
	// After authentication, the session no longer has the email key, so CSRF will fail
	resp := resend2faSuite(srv, user.Email, server.XSRF.Token(user.Email), cookie)

	// Should redirect to expired since session is already completed (no email in session)
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status code: %v", resp.StatusCode)
	}
}

func TestParseOrgInviteIDFromURLValid(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Use the server's IDHasher
	encodedID := server.IDHasher.Encrypt(123)
	rawURL := "https://portal.example.com/" + common.OrgInviteEndpoint + "/" + encodedID + "/" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != 123 {
		t.Errorf("Expected 123, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLNoPrefixMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// URL without orginvite prefix
	rawURL := "https://portal.example.com/other/path/" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != -1 {
		t.Errorf("Expected -1, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLNoSuffix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	encodedID := server.IDHasher.Encrypt(123)
	// URL without /signup suffix
	rawURL := "https://portal.example.com/" + common.OrgInviteEndpoint + "/" + encodedID

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != -1 {
		t.Errorf("Expected -1, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Use an invalid encoded ID
	rawURL := "https://portal.example.com/" + common.OrgInviteEndpoint + "/invalid-id/" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != -1 {
		t.Errorf("Expected -1 for invalid ID, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLEmptyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Path with empty ID segment
	rawURL := "https://portal.example.com/" + common.OrgInviteEndpoint + "//" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != -1 {
		t.Errorf("Expected -1 for empty ID, got %d", result)
	}
}

func TestParseOrgInviteIDFromURLRelativePath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	encodedID := server.IDHasher.Encrypt(456)
	// Relative URL
	rawURL := "/" + common.OrgInviteEndpoint + "/" + encodedID + "/" + common.RegisterEndpoint

	result := server.parseOrgInviteIDFromURL(rawURL)

	if result != 456 {
		t.Errorf("Expected 456, got %d", result)
	}
}
