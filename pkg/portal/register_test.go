package portal

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	emailpkg "github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
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

	stubMailer := server.Mailer.(*emailpkg.StubMailer)
	resp = twoFactorSuite(srv, email, server.XSRF.Token(email), stubMailer.LastCode, cookie)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("unexpected post twofactor code: %v", resp.StatusCode)
	}

	if location, _ := resp.Location(); location.String() != "/" {
		t.Errorf("unexpected redirect: %v", location)
	}

	ctx := context.TODO()
	user, err := store.Impl().FindUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}

	if user.Email != email {
		t.Errorf("Unexpected user email")
	}
}
