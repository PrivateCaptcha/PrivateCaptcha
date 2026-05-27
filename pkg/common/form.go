package common

import (
	"fmt"
	"net/http"
	"time"
)

const maxFormRedirects = 10

func NewFormHTTPClient(verifier FormURLVerifier) *http.Client {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFormRedirects {
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
	}

	return client
}
