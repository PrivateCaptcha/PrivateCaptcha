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

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
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

func loginSuite(srv *http.ServeMux, email, token string, cookies ...*http.Cookie) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamPortalSolution, "captchaSolution")

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
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

	code, err := portal_tests.TwoFactorCodeFromEmail(user.Email)
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

	code, err := portal_tests.TwoFactorCodeFromEmail(user.Email)
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt < maxFailedAttempts; attempt++ {
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

	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), code+1, cookie)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read limited response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Too many failed attempts. Please start again.") {
		t.Fatalf("expected fifth failure to exhaust the challenge: status %v, body %s", resp.StatusCode, body)
	}
	if cookies := resp.Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("exhausted challenge did not clear its cookie: %v", cookies)
	}

	mailer, ok := server.Mailer.(*email.StubMailer)
	if !ok {
		t.Fatalf("mailer type = %T, want *email.StubMailer", server.Mailer)
	}
	sentBeforeResend := mailer.TwoFactorCount()
	resp = resend2faSuite(srv, user.Email, server.XSRF.Token(user.Email), cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("exhausted challenge resend status = %v, want redirect", resp.StatusCode)
	}
	if cookies := resp.Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("exhausted resend did not clear its cookie: %v", cookies)
	}
	if sent := mailer.TwoFactorCount(); sent != sentBeforeResend {
		t.Fatalf("exhausted resend sent %d additional codes", sent-sentBeforeResend)
	}

	sentBeforeLogin := mailer.TwoFactorCount()
	resp = loginSuite(srv, user.Email, server.XSRF.Token(""), cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("exhausted challenge login status = %v, want redirect", resp.StatusCode)
	}
	if cookies := resp.Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("exhausted login did not clear its cookie: %v", cookies)
	}
	if sent := mailer.TwoFactorCount(); sent != sentBeforeLogin {
		t.Fatalf("exhausted login sent %d additional codes", sent-sentBeforeLogin)
	}

	sentBeforeRegistration := mailer.TwoFactorCount()
	registrationEmail := t.Name() + "-registration@privatecaptcha.com"
	resp = registerSuite(srv, "Registrant", registrationEmail, server.XSRF.Token(""), cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("exhausted challenge registration status = %v, want redirect", resp.StatusCode)
	}
	if cookies := resp.Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("exhausted registration did not clear its cookie: %v", cookies)
	}
	if sent := mailer.TwoFactorCount(); sent != sentBeforeRegistration {
		t.Fatalf("exhausted registration sent %d additional codes", sent-sentBeforeRegistration)
	}
}

func TestRegistrationAttemptLimitClearsCookieBeforeCSRF(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	emailAddress := t.Name() + "@privatecaptcha.com"
	resp := registerSuite(srv, "Registrant", emailAddress, server.XSRF.Token(""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected registration status code: %v", resp.StatusCode)
	}
	cookieIndex := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if cookieIndex == -1 {
		t.Fatal("cannot find registration session cookie")
	}
	cookie := resp.Cookies()[cookieIndex]
	code, err := portal_tests.TwoFactorCodeFromEmail(emailAddress)
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt < maxFailedAttempts; attempt++ {
		resp = twoFactorSuite(srv, emailAddress, server.XSRF.Token(emailAddress), code+1, cookie)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("failed attempt %d status = %v, want OK", attempt, resp.StatusCode)
		}
	}

	mailer, ok := server.Mailer.(*email.StubMailer)
	if !ok {
		t.Fatalf("mailer type = %T, want *email.StubMailer", server.Mailer)
	}
	sentBeforeExhaustion := mailer.TwoFactorCount()
	resp = twoFactorSuite(srv, emailAddress, server.XSRF.Token("wrong-email@example.com"), code+1, cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("cap-reaching registration status = %v, want redirect", resp.StatusCode)
	}
	if location, err := resp.Location(); err != nil || location.Path != "/"+common.ExpiredEndpoint {
		t.Fatalf("cap-reaching registration redirect = (%v, %v), want /%s", location, err, common.ExpiredEndpoint)
	}
	if cookies := resp.Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("cap-reaching registration did not clear its cookie: %v", cookies)
	}
	if sent := mailer.TwoFactorCount(); sent != sentBeforeExhaustion {
		t.Fatalf("cap-reaching registration sent %d additional codes", sent-sentBeforeExhaustion)
	}
	if _, err := store.Impl().FindUserByEmail(t.Context(), emailAddress); err == nil {
		t.Fatal("cap-reaching registration with invalid CSRF created an account")
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

	originalCode, err := portal_tests.TwoFactorCodeFromEmail(user.Email)
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
	newCode, err := portal_tests.TwoFactorCodeFromEmail(user.Email)
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
