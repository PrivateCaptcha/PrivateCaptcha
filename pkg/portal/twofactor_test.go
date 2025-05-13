package portal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	emailpkg "github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

func authenticateSuite(ctx context.Context, email string, srv *http.ServeMux) (*http.Cookie, error) {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(""))
	form.Add(common.ParamEmail, email)

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	stubMailer, ok := server.Mailer.(*emailpkg.StubMailer)
	if !ok {
		return nil, errors.New("failed to cast Mailer to StubMailer")
	}
	resp := w.Result()
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == server.Session.CookieName })
	if idx == -1 {
		return nil, errors.New("cannot find session cookie in response")
	}
	cookie := resp.Cookies()[idx]

	form = url.Values{}
	form.Add(common.ParamCSRFToken, server.XSRF.Token(email))
	form.Add(common.ParamEmail, email)
	form.Add(common.ParamVerificationCode, strconv.Itoa(stubMailer.LastCode))

	// now send the 2fa request
	req = httptest.NewRequest("POST", "/"+common.TwoFactorEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		return nil, fmt.Errorf("Unexpected post twofactor code: %v", w.Code)
	}

	slog.Log(ctx, common.LevelTrace, "Looks like we are authenticated", "code", w.Code)

	return cookie, nil
}

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

	cookie, err := authenticateSuite(ctx, user.Email, srv)
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
