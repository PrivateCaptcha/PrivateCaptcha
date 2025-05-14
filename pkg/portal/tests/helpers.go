package tests

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	emailpkg "github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// courtesy of https://martinfowler.com/articles/tdd-html-templates.html
func AssertWellFormedHTML(t *testing.T, buf bytes.Buffer) {
	data := buf.Bytes()
	// special handling for Alpine.js, otherwise we get XML parsing error "attribute expected"
	data = bytes.ReplaceAll(data, []byte(" @click="), []byte(" click="))
	data = bytes.ReplaceAll(data, []byte(" hx-on::"), []byte(" hx-on-"))

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity
	for {
		token, err := decoder.Token()
		switch err {
		case io.EOF:
			return // We're done, it's valid!
		case nil:
			// do nothing
		default:
			fmt.Println(buf.String())
			t.Fatalf("Error parsing html: %s, %v", err, token)
		}
	}
}

func ParseHTML(t *testing.T, buf bytes.Buffer) *goquery.Document {
	AssertWellFormedHTML(t, buf)
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		// if parsing fails, we stop the test here with t.FatalF
		t.Fatalf("Error rendering template %s", err)
	}
	return document
}

func Text(node *html.Node) string {
	// A little mess due to the fact that goquery has
	// a .Text() method on Selection but not on html.Node
	sel := goquery.Selection{Nodes: []*html.Node{node}}
	return strings.TrimSpace(sel.Text())
}

func AuthenticateSuite(ctx context.Context, email string, srv *http.ServeMux, xsrf *common.XSRFMiddleware, cookieName string, stubMailer *emailpkg.StubMailer) (*http.Cookie, error) {
	form := url.Values{}
	form.Add(common.ParamCSRFToken, xsrf.Token(""))
	form.Add(common.ParamEmail, email)

	// Send the POST request
	req := httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	idx := slices.IndexFunc(resp.Cookies(), func(c *http.Cookie) bool { return c.Name == cookieName })
	if idx == -1 {
		return nil, errors.New("cannot find session cookie in response")
	}
	cookie := resp.Cookies()[idx]

	form = url.Values{}
	form.Add(common.ParamCSRFToken, xsrf.Token(email))
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
