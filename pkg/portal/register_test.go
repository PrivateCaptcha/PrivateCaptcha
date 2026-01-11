package portal

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
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

	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find session cookie in response")
	}
	cookie := resp.Cookies()[idx]

	ctx := t.Context()
	code, err := portal_tests.TwoFactorCodeFromSession(ctx, cookie.Value, server.Sessions.Store)
	if err != nil {
		t.Fatal(err)
	}

	resp = twoFactorSuite(srv, email, server.XSRF.Token(email), code, cookie)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	if location, _ := resp.Location(); location.String() != "/" {
		t.Errorf("unexpected redirect: %v", location)
	}

	user, err := store.Impl().FindUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}

	if user.Email != email {
		t.Errorf("Unexpected user email")
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
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, t.Name()+"@privatecaptcha.com")
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

	body := w.Body.String()
	if !strings.Contains(body, "captcha") {
		t.Error("Expected error message about captcha")
	}
}
