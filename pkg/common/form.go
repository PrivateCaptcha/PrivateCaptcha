package common

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const maxFormRedirects = 10

func NewFormHTTPClient() *http.Client {
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: noFormRedirects,
	}

	if transport, ok := http.DefaultTransport.(*http.Transport); ok && (transport != nil) {
		clone := transport.Clone()
		client.Transport = clone
		clone.DisableKeepAlives = true
	}

	return client
}

func NewFormRedirectHTTPClient(verifier FormURLVerifier, redirectCount int16) *http.Client {
	allowedRedirects := min(int(redirectCount), maxFormRedirects)
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > allowedRedirects {
				return fmt.Errorf("stopped after %d redirects", maxFormRedirects)
			}

			if err := verifier.VerifyURL(req.Context(), req.URL.String()); err != nil {
				return fmt.Errorf("unsafe form redirect: %w", err)
			}
			return nil
		},
	}

	if transport, ok := http.DefaultTransport.(*http.Transport); ok && (transport != nil) {
		clone := transport.Clone()
		clone.DialContext = verifier.DialContext
		client.Transport = clone
		clone.DisableKeepAlives = true
	}

	return client
}

func noFormRedirects(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid-url]"
	}

	// Drop username/password: https://user:pass@example.com
	u.User = nil

	// Drop fragment: https://example.com/path#token
	u.Fragment = ""

	// Redact all query values, keeping only parameter names.
	// This preserves debugging value without leaking secrets.
	q := u.Query()
	for key := range q {
		q[key] = []string{"[redacted]"}
	}
	u.RawQuery = q.Encode()

	return u.String()
}
