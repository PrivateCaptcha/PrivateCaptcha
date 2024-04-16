package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
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
				if a.Key == "name" && a.Val == common.ParamCsrfToken {
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

	server.getLogin(rr, req)

	// check if the status code is 200
	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}

	token, err := parseCsrfToken(rr.Body.String())
	if (err != nil) || (token == "") {
		t.Errorf("failed to parse csrf token: %v", err)
	}

	if !server.XSRF.VerifyToken(token, "", actionLogin) {
		t.Error("Failed to verify token in Login form")
	}
}
