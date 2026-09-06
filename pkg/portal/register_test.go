package portal

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type blockingRegistrationJobs struct {
	db.UserJobs
	job common.OneOffJob
}

func (j *blockingRegistrationJobs) CheckRegistration(*session.Session, *http.Request) common.OneOffJob {
	return j.job
}

type blockingRegistrationCheckJob struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (j *blockingRegistrationCheckJob) Name() string                { return "BlockingRegistrationCheck" }
func (j *blockingRegistrationCheckJob) InitialPause() time.Duration { return 0 }
func (j *blockingRegistrationCheckJob) NewParams() any              { return struct{}{} }
func (j *blockingRegistrationCheckJob) RunOnce(context.Context, any) error {
	close(j.started)
	<-j.release
	close(j.finished)
	return nil
}

func registerSuite(srv *http.ServeMux, name, email, token string, cookies ...*http.Cookie) *http.Response {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, token)
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamName, name)
	form.Add(common.ParamTerms, "true")
	form.Add(common.ParamPortalSolution, "captchaSolution")

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.RegisterEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
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
	code, err := portal_tests.TwoFactorCodeFromEmail(email)
	if err != nil {
		t.Fatal(err)
	}

	resp = twoFactorSuite(srv, email, server.XSRF.Token(email), code, cookie)

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

	if user.Email != email {
		t.Errorf("Unexpected user email")
	}
}

func TestPostRegisterRunsRegistrationCheckAsynchronously(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	job := &blockingRegistrationCheckJob{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	originalJobs := server.Jobs
	server.Jobs = &blockingRegistrationJobs{UserJobs: originalJobs, job: job}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(job.release) }) }
	t.Cleanup(func() {
		release()
		server.Jobs = originalJobs
	})

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	response := make(chan *http.Response, 1)
	go func() {
		response <- registerSuite(srv, "Async User", t.Name()+"@privatecaptcha.com", server.XSRF.Token(""))
	}()

	select {
	case <-job.started:
	case <-time.After(5 * time.Second):
		t.Fatal("registration check did not start")
	}
	select {
	case resp := <-response:
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("registration status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	case <-time.After(time.Second):
		release()
		<-response
		t.Fatal("registration response waited for the registration check")
	}

	release()
	select {
	case <-job.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("registration check did not finish")
	}
}

func TestRegistrationInvalidCSRFDoesNotConsumeChallenge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	email := t.Name() + "@privatecaptcha.com"
	resp := registerSuite(srv, "Foo Bar", email, server.XSRF.Token(""))
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find registration session cookie")
	}
	cookie := resp.Cookies()[idx]
	code, err := portal_tests.TwoFactorCodeFromEmail(email)
	if err != nil {
		t.Fatal(err)
	}

	resp = twoFactorSuite(srv, email, server.XSRF.Token("wrong-email@example.com"), code, cookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("invalid CSRF status = %d, want redirect", resp.StatusCode)
	}
	location, err := resp.Location()
	if err != nil || location.Path != "/"+common.ExpiredEndpoint {
		t.Fatalf("invalid CSRF redirect = (%v, %v), want /%s", location, err, common.ExpiredEndpoint)
	}
	if _, err := store.Impl().FindUserByEmail(t.Context(), email); err == nil {
		t.Fatal("invalid CSRF created an account")
	}

	resp = twoFactorSuite(srv, email, server.XSRF.Token(email), code, cookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("valid CSRF retry status = %d, want redirect", resp.StatusCode)
	}
	if _, err := store.Impl().FindUserByEmail(t.Context(), email); err != nil {
		t.Fatalf("valid CSRF retry did not create an account: %v", err)
	}
}

func TestRegistrationCheckRequiresVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	resp := registerSuite(srv, "Spam User", spammerEmail, server.XSRF.Token(""))
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Sessions.CookieName })
	if idx == -1 {
		t.Fatal("cannot find registration session cookie")
	}
	cookie := resp.Cookies()[idx]
	code, err := portal_tests.TwoFactorCodeFromEmail(spammerEmail)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(cookie)
		sess, resolveErr := server.Sessions.Get(req)
		if resolveErr == nil {
			authority, ok := sess.Authority()
			if ok && authority.VerifyRegistration {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("registration screening did not update cached Authority: %v", resolveErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp = twoFactorSuite(srv, spammerEmail, server.XSRF.Token(spammerEmail), code, cookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("verification-required status = %d, want redirect", resp.StatusCode)
	}
	location, err := resp.Location()
	if err != nil || location.Path != "/"+common.AccountVerifyEndpoint {
		t.Fatalf("verification-required redirect = (%v, %v), want /%s", location, err, common.AccountVerifyEndpoint)
	}
	if _, err := store.Impl().FindUserByEmail(t.Context(), spammerEmail); err == nil {
		t.Fatal("verification-required registration created an account")
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

	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, t.Name()+"@privatecaptcha.com")
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

	body := w.Body.String()
	if !strings.Contains(body, captchaVerificationFailed) {
		t.Errorf("Expected captcha verification failed error message, got: %s", body)
	}
}
