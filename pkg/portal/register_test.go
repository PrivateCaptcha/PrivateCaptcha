package portal

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

func registerSuite(srv *http.ServeMux, name, email, token string) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, name)
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	return w.Result()
}

func TestPostRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	email := t.Name() + "@privatecaptcha.com"
	name := "Foo Bar"
	resp := registerSuite(srv, name, email, server.XSRF.Token(""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Unexpected login status code: %v", resp.StatusCode)
	}

	cookie := responseCookieForTest(t, resp, server.Sessions.CookieName)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	token, err := parseCsrfToken(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if !server.XSRF.VerifyToken(token, cookie.Value) {
		t.Fatal("rendered registration CSRF token is not bound to pending SID")
	}

	ctx := t.Context()
	code, ok := testMailer.TwoFactorCode(email)
	if !ok {
		t.Fatalf("registration code was not sent to %s", email)
	}

	wrongCode := invalidVerificationCode(code)
	resp = twoFactorSuite(srv, email, token, wrongCode, cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registration invalid attempt status = %d, want 200", resp.StatusCode)
	}
	resp = resend2faSuite(srv, email, token, cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registration resend status = %d, want 200", resp.StatusCode)
	}
	newCode, ok := testMailer.TwoFactorCode(email)
	if !ok || newCode == code {
		t.Fatal("registration resend did not deliver a replacement code")
	}
	resp = twoFactorSuite(srv, email, token, code, cookie)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("replaced registration code did not fail")
	}
	code = newCode

	resp = twoFactorSuite(srv, email, token, code, cookie)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	location, err := resp.Location()
	if err != nil {
		t.Fatalf("Expected redirect response but got error: %v", err)
	}

	user, err := store.Impl().FindUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}

	orgs, err := store.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil || len(orgs) == 0 {
		t.Fatalf("Expected user to have an organization after registration, err: %v", err)
	}

	expectedPath := fmt.Sprintf("/?%s=true", common.ParamOnboarding)
	if path := location.String(); path != expectedPath {
		t.Errorf("unexpected redirect: %v, expected: %v", path, expectedPath)
	}

	rotatedCookie := responseCookieForTest(t, resp, server.Sessions.CookieName)
	if rotatedCookie.Value == cookie.Value {
		t.Fatal("registration did not rotate the pending SID")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		successor, successorErr := dbgen.New(store.Pool).GetSessionByID(ctx, rotatedCookie.Value)
		if successorErr == nil && successor.State == dbgen.SessionStateAuthenticated && successor.UserID.Valid && successor.UserID.Int32 == user.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registration successor did not finalize: row=%+v err=%v", successor, successorErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPostRegisterFlaggedRequiresVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	resp := registerSuite(srv, "Flagged User", spammerEmail, server.XSRF.Token(""))
	cookie := responseCookieForTest(t, resp, server.Sessions.CookieName)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	token, err := parseCsrfToken(string(body))
	if err != nil {
		t.Fatal(err)
	}

	code, ok := testMailer.TwoFactorCode(spammerEmail)
	if !ok {
		t.Fatal("flagged registration code was not sent")
	}
	resp = twoFactorSuite(srv, spammerEmail, token, code, cookie)
	resp.Body.Close()
	location, _ := resp.Location()
	if resp.StatusCode != http.StatusSeeOther || location.Path != "/"+common.AccountVerifyEndpoint {
		t.Fatalf("flagged registration response = (status %d, location %v)", resp.StatusCode, location)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatal("flagged registration received a processing cookie")
	}
}

func TestGetRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	req := httptest.NewRequest("GET", "/"+common.RegisterEndpoint, nil)
	w := httptest.NewRecorder()

	viewModel, err := server.getRegister(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != loginTemplate {
		t.Errorf("Expected view to be %s, got %s", loginTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*loginRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *loginRenderContext, got %T", viewModel.Model)
	}

	if !renderCtx.IsRegister {
		t.Error("Expected IsRegister to be true")
	}

	if len(renderCtx.Token) == 0 {
		t.Error("Expected CSRF token to be populated")
	}
}

func TestPostRegisterEmptyName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Empty name
	email := t.Name() + "@privatecaptcha.com"
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, "")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	// Should show error about name being too short
	body := w.Body.String()
	if !strings.Contains(body, "name") && !strings.Contains(body, "longer") {
		t.Error("Expected error message about name")
	}
}

func TestPostRegisterShortName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Very short name (less than 3 chars)
	email := t.Name() + "@privatecaptcha.com"
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, "AB")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "longer") {
		t.Error("Expected error message about name being too short")
	}
}

func TestPostRegisterInvalidNameChars(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Name with invalid characters
	email := t.Name() + "@privatecaptcha.com"
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, "Test@User#123")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "invalid") {
		t.Error("Expected error message about invalid name characters")
	}
}

func TestPostRegisterMalformedEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Malformed email
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, "not-an-email")
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "not valid") {
		t.Error("Expected error message about invalid email")
	}
}

func TestPostRegisterMissingTerms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// No terms accepted
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, t.Name()+"@privatecaptcha.com")
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamPortalSolution, "captchaSolution")
	// No terms

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Missing terms should redirect to bad request
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status, got %v", w.Code)
	}
}

func TestPostRegisterMissingCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// No captcha solution
	email := t.Name() + "@privatecaptcha.com"
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")
	// No captcha solution

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	if len(w.Result().Cookies()) != 0 {
		t.Fatal("missing CAPTCHA response created a registration cookie")
	}
	if _, ok := testMailer.TwoFactorCode(email); ok {
		t.Fatal("missing CAPTCHA response sent a registration code")
	}
}

func TestPostRegisterExistingEmail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create an existing user first
	existingUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_existing", testPlan)
	if err != nil {
		t.Fatalf("Failed to create existing account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to register with the same email
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, existingUser.Email)
	form.Add(common.ParamName, "Another User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	// Check for specific error message constant
	if !strings.Contains(body, emailAlreadyRegisteredError) {
		t.Errorf("Expected error message '%s', got body: %s", emailAlreadyRegisteredError, body)
	}
}

func TestPostRegisterExistingEmailCaseInsensitive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	// Create an existing user first
	existingUser, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_EXIsTiNG", testPlan)
	if err != nil {
		t.Fatalf("Failed to create existing account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to register with the same email but different case
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, strings.ToUpper(existingUser.Email))
	form.Add(common.ParamName, "Another User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	body := w.Body.String()
	// Check for specific error message constant
	if !strings.Contains(body, emailAlreadyRegisteredError) {
		t.Errorf("Expected error message '%s', got body: %s", emailAlreadyRegisteredError, body)
	}
}

func TestGetRegisterDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Temporarily disable registration
	server.canRegister.Store(false)
	defer server.canRegister.Store(true)

	req := httptest.NewRequest("GET", "/"+common.RegisterEndpoint, nil)
	w := httptest.NewRecorder()

	viewModel, err := server.getRegister(w, req)

	if err != errRegistrationDisabled {
		t.Errorf("Expected errRegistrationDisabled, got: %v", err)
	}

	if viewModel != nil {
		t.Error("Expected nil ViewModel when registration is disabled")
	}
}

func TestPostRegisterDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Temporarily disable registration
	server.canRegister.Store(false)
	defer server.canRegister.Store(true)

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	// Try to register
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, "disabled-registration@privatecaptcha.com")
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Should be redirected to an error page when registration is disabled
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status when registration disabled, got %v", w.Code)
	}
}

func TestPostRegisterInvalidCaptcha(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Temporarily make the puzzle engine return a failed verify result
	originalResult := server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result
	server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result = &puzzle.VerifyResult{Error: puzzle.InvalidSolutionError}
	defer func() {
		server.PuzzleEngine.(*portal_tests.StubPuzzleEngine).Result = originalResult
	}()

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	email := t.Name() + "@privatecaptcha.com"
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, "Test User")
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "invalid-captcha-solution")

	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %v", w.Code)
	}

	if len(w.Result().Cookies()) != 0 {
		t.Fatal("invalid CAPTCHA response created a registration cookie")
	}
	if _, ok := testMailer.TwoFactorCode(email); ok {
		t.Fatal("invalid CAPTCHA response sent a registration code")
	}
}
