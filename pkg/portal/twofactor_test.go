package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	_ = server.Setup(srv, portalDomain(), common.NoopMiddleware)

	ctx := context.TODO()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name())
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
