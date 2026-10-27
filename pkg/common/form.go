package common

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func NewFormHTTPClient(verifier FormURLVerifier, redirectCount int16) *http.Client {
	const maxFormRedirects int16 = 10

	allowedRedirects := int(max(0, min(redirectCount, maxFormRedirects)))
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > allowedRedirects {
				if allowedRedirects == 0 {
					return http.ErrUseLastResponse
				}

				return fmt.Errorf("stopped after %d redirects", allowedRedirects)
			}

			if err := verifier.VerifyURL(req.Context(), req.URL.String()); err != nil {
				return fmt.Errorf("unsafe form redirect: %w", err)
			}

			return nil
		},
	}

	if transport, ok := http.DefaultTransport.(*http.Transport); ok && (transport != nil) {
		clone := transport.Clone()
		// Form URLs are verified by resolving them and dialling the verified IP
		// literal, which is what makes the check rebinding-proof. An HTTP proxy
		// would receive the request instead and resolve the hostname itself, so
		// the verification would only ever apply to the proxy address: never
		// route these requests through a proxy, even if one is configured in the
		// environment.
		clone.Proxy = nil
		clone.DialContext = verifier.DialContext
		client.Transport = clone
		clone.DisableKeepAlives = true
	}

	return client
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
