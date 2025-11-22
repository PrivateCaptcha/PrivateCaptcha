package portal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
)

func createAPIKeySuite(srv *http.ServeMux, csrfToken string, cookie *http.Cookie, name string, days int) *http.Response {
	// Send POST request to create a new API key
	form := url.Values{}
	form.Set(common.ParamCSRFToken, csrfToken)
	form.Set(common.ParamName, name)
	form.Set(common.ParamDays, strconv.Itoa(days))

	req := httptest.NewRequest("POST", "/settings/tab/apikeys/new", strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	return w.Result()
}

func TestCreateAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(context.TODO(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	name := "My API Key"
	resp := createAPIKeySuite(srv, csrfToken, cookie, name, 90)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if keysLen := len(keys); keysLen != 1 {
		t.Errorf("Unexpected number of API keys: %v", keysLen)
	}

	_ = createAPIKeySuite(srv, csrfToken, cookie, name, 180)
	keys, err = store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keysLen := len(keys); keysLen != 1 {
		t.Errorf("Duplicate key was created. Keys count: %v", keysLen)
	}
}

func TestDeleteAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(context.TODO(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	key, _, err := store.Impl().CreateAPIKey(ctx, user, "My API Key", time.Now(), 24*time.Hour, 10.0)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	req := httptest.NewRequest("DELETE", fmt.Sprintf("/apikeys/%v", server.IDHasher.Encrypt(int(key.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keysLen := len(keys); keysLen != 0 {
		t.Errorf("API key was not deleted. Keys count: %v", keysLen)
	}
}

func TestRotateAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := common.TraceContext(context.TODO(), t.Name())
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions.CookieName, server.Mailer.(*email.StubMailer))
	if err != nil {
		t.Fatal(err)
	}

	tnow := time.Now().UTC()
	key, _, err := store.Impl().CreateAPIKey(ctx, user, "My API Key", tnow.Add(-24*time.Hour), 23*time.Hour, 10.0)
	if err != nil {
		t.Fatal(err)
	}
	secretOld := db.UUIDToSecret(key.ExternalID)

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	req := httptest.NewRequest("POST", fmt.Sprintf("/apikeys/%v", server.IDHasher.Encrypt(int(key.ID))), nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	keys, err := store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keysLen := len(keys); keysLen != 1 {
		t.Errorf("Unexpected number of API keys: %v", keysLen)
	}
	if !keys[0].ExpiresAt.Valid || !keys[0].ExpiresAt.Time.After(tnow.Add(22*time.Hour)) {
		t.Errorf("Key expiration was not rotated")
	}

	if secret := db.UUIDToSecret(keys[0].ExternalID); secret == secretOld {
		t.Error("Key external ID was not rotated")
	}
}
