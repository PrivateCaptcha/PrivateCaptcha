package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
)

func TestContactSupport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.TODO()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name())
	if err != nil {
		t.Fatalf("Failed to create user account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(srv, portalDomain(), common.NoopMiddleware)

	cookie, err := authenticateSuite(ctx, user.Email, srv)
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamCategory, "0")
	form.Set(common.ParamMessage, "Nothing works! Bugs everywhere!")
	form.Set(common.ParamSubject, "Subject is not too long")

	req := httptest.NewRequest("POST", "/"+common.SupportEndpoint, strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected status code: %v", resp.StatusCode)
	}
}
