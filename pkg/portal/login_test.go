package portal

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

func parseCsrfToken(body string) (string, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return "", err
	}

	var csrfToken string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			isCsrfElement := false
			token := ""

			for _, a := range n.Attr {
				if a.Key == "name" && a.Val == common.ParamCSRFToken {
					isCsrfElement = true
				}

				if a.Key == "type" && a.Val == "hidden" {
					for _, a := range n.Attr {
						if a.Key == "value" {
							token = a.Val
						}
					}
				}
			}

			if isCsrfElement && (len(token) > 0) && (len(csrfToken) == 0) {
				csrfToken = token
			}
		}

		if len(csrfToken) == 0 {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}
	}
	f(doc)

	return csrfToken, nil
}

func TestGetLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)

	rr := httptest.NewRecorder()

	server.Handler(server.getLogin).ServeHTTP(rr, req)

	// check if the status code is 200
	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}

	token, err := parseCsrfToken(rr.Body.String())
	if (err != nil) || (token == "") {
		t.Errorf("failed to parse csrf token: %v", err)
	}

	if !server.XSRF.VerifyToken(token, "") {
		t.Error("Failed to verify token in Login form")
	}
}

func TestGetLoginMaintenance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)

	server.maintenanceMode.Store(true)
	defer server.maintenanceMode.Store(false)

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v", resp.StatusCode, http.StatusSeeOther)
	}
}

func TestPostLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()

	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("failed to create new account: %v", err)
	}

	// Get the CSRF token
	req := httptest.NewRequest("GET", "/"+common.LoginEndpoint, nil)
	rr := httptest.NewRecorder()
	server.Handler(server.getLogin).ServeHTTP(rr, req)
	csrfToken, err := parseCsrfToken(rr.Body.String())
	if err != nil {
		t.Fatalf("failed to parse CSRF token: %v", err)
	}

	// Prepare the form data
	form := url.Values{}
	form.Add(common.ParamCSRFToken, csrfToken)
	form.Add(common.ParamEmail, user.Email)
	form.Add(common.ParamPortalSolution, "captcha solution")

	// Send the POST request
	req = httptest.NewRequest("POST", "/"+common.LoginEndpoint, bytes.NewBufferString(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	rr = httptest.NewRecorder()
	server.postLogin(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Unexpected post login code: %v", rr.Code)
	}

	// Check if the two-factor code is set in the StubMailer
	stubMailer, ok := server.Mailer.(*email.StubMailer)
	if !ok {
		t.Fatal("failed to cast Mailer to StubMailer")
	}
	if stubMailer.LastCode == 0 {
		t.Error("two-factor code not set in StubMailer")
	}
}
