package paddle

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
)

var (
	// ErrMissingSignature is returned when the signature is missing.
	ErrMissingSignature = errors.New("missing signature")

	// ErrInvalidSignatureFormat is returned when the signature format is invalid.
	ErrInvalidSignatureFormat = errors.New("invalid signature format")
)

// signatureRegexp matches the Paddle-Signature header format, e.g.:
//
//	ts=1671552777;h1=eb4d0dc8853be92b7f063b9f3ba5233eb920a09459b6e6b2c26705b4364db151
//
// More information can be found here: https://developer.paddle.com/webhooks/signature-verification.
var signatureRegexp = regexp.MustCompile(`^ts=(\d+);h1=(\w+)$`)

// WebhookVerifier is used to verify webhook requests from Paddle.
type WebhookVerifier struct {
	secretKey []byte
}

// NewWebhookVerifier creates a new WebhookVerifier with the given secret key.
func NewWebhookVerifier(secretKey string) *WebhookVerifier {
	return &WebhookVerifier{secretKey: []byte(secretKey)}
}

// Verify verifies the signature of a webhook request.
func (wv *WebhookVerifier) Verify(req *http.Request) (bool, error) {
	sig := req.Header.Get("Paddle-Signature")
	if sig == "" {
		return false, ErrMissingSignature
	}

	matches := signatureRegexp.FindAllStringSubmatch(sig, -1)
	if len(matches) != 1 || len(matches[0]) != 3 {
		return false, ErrInvalidSignatureFormat
	}

	ts := matches[0][1]
	h1 := matches[0][2]

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return false, err
	}

	req.Body = io.NopCloser(bytes.NewBuffer(body))

	mac := hmac.New(sha256.New, wv.secretKey)
	mac.Write([]byte(ts))
	mac.Write([]byte(":"))
	mac.Write(body)

	generatedMAC := mac.Sum(nil)

	dst, err := hex.DecodeString(h1)
	if err != nil {
		return false, err
	}

	return hmac.Equal(dst, generatedMAC), nil
}

// Middleware returns a middleware that verifies the signature of a webhook
// request.
func (wv *WebhookVerifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, err := wv.Verify(r)
		if err != nil && (errors.Is(err, ErrMissingSignature) || errors.Is(err, ErrInvalidSignatureFormat)) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if !ok {
			http.Error(w, "signature mismatch", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
