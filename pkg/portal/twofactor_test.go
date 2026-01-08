package portal

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
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

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
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

	stubMailer := server.Mailer.(*email.StubMailer)
	resp = twoFactorSuite(srv, user.Email, server.XSRF.Token(user.Email), stubMailer.LastCode, cookie)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	if location, _ := resp.Location(); location.String() != "/" {
		t.Errorf("unexpected redirect: %v", location)
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

	stubMailer := server.Mailer.(*email.StubMailer)
	originalCode := stubMailer.LastCode

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
	newCode := stubMailer.LastCode
	if newCode == 0 {
		t.Error("New 2FA code was not generated")
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
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
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
