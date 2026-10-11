package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func TestErrorPage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	testCases := []struct {
		code int
	}{
		{http.StatusForbidden},
		{http.StatusNotFound},
		{http.StatusUnauthorized},
		{http.StatusServiceUnavailable},
		{http.StatusInternalServerError},
	}

	for _, tc := range testCases {
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			codeStr := strconv.Itoa(tc.code)
			req := httptest.NewRequest("GET", "/error/"+codeStr, nil)
			req.SetPathValue(common.ParamCode, codeStr)

			w := httptest.NewRecorder()
			server.error(w, req)

			if w.Code != tc.code {
				t.Errorf("Expected status code %d, got %d", tc.code, w.Code)
			}
		})
	}
}

func TestSignedInErrorPageIncludesLogoutCSRF(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	ctx = context.WithValue(ctx, common.LoggedInContextKey, true)
	req := httptest.NewRequest(http.MethodGet, "/error/500", nil).WithContext(ctx)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	server.renderError(ctx, w, req, http.StatusInternalServerError)

	assertLogoutCSRF(t, w.Body, user.ID)
}

func TestSignedInErrorPageOnPublicChainIncludesLogoutCSRF(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/error/500", nil)
	req.SetPathValue(common.ParamCode, "500")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	server.error(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %v, want %v", w.Code, http.StatusInternalServerError)
	}

	assertLogoutCSRF(t, w.Body, user.ID)
}

func TestAnonymousErrorPageOmitsLogoutCSRF(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	for _, code := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		req := httptest.NewRequest(http.MethodGet, "/error/"+strconv.Itoa(code), nil)
		req.SetPathValue(common.ParamCode, strconv.Itoa(code))
		w := httptest.NewRecorder()
		server.error(w, req)

		if w.Code != code {
			t.Errorf("status = %v, want %v", w.Code, code)
		}

		document := portal_tests.ParseHTML(t, w.Body)
		if _, ok := document.Find("body").Attr("hx-headers"); ok {
			t.Errorf("anonymous error page (%d) must not include inherited HTMX CSRF headers", code)
		}
	}
}

func TestExpiredPageIncludesLogoutCSRFForAuthenticatedUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/expired", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	server.expired(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %v, want %v", w.Code, http.StatusOK)
	}

	assertLogoutCSRF(t, w.Body, user.ID)
}

func TestNotFoundHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	server.notFound(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestExpiredHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/expired", nil)
	w := httptest.NewRecorder()

	server.expired(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestPostClientSideError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(t.Context(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"error": "test error", "stack": "test stack trace"}`
	req := httptest.NewRequest("POST", "/error", strings.NewReader(body))
	req.Header.Set(common.HeaderContentType, common.ContentTypeJSON)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
}

func TestErrorPageInvalidCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/error/99999", nil)
	req.SetPathValue(common.ParamCode, "99999")

	w := httptest.NewRecorder()
	server.error(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status code %d for invalid error code, got %d", http.StatusInternalServerError, w.Code)
	}
}
