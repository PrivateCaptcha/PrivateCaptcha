package api

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

type stubFormURLResolver struct {
	addresses map[string][]string
	errors    map[string]error
	calls     map[string]int
}

func (r *stubFormURLResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	r.calls[host]++
	if err := r.errors[host]; err != nil {
		return nil, err
	}

	addresses := r.addresses[host]
	result := make([]net.IPAddr, 0, len(addresses))
	for _, rawIP := range addresses {
		result = append(result, net.IPAddr{IP: net.ParseIP(rawIP)})
	}

	return result, nil
}

func newTestPublicFormURLVerifier(t *testing.T, resolver *stubFormURLResolver) *PublicFormURLVerifier {
	t.Helper()

	cache, err := db.NewMemoryCache[string, bool]("test-form-url-verifier", 1000, false, time.Minute, time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	return &PublicFormURLVerifier{
		VerifiedHosts: cache,
		Resolver:      resolver,
	}
}

func TestPublicFormURLVerifierAllowsPublicURLs(t *testing.T) {
	ctx := context.Background()
	resolver := &stubFormURLResolver{
		addresses: map[string][]string{
			"example.com": {"93.184.216.34"},
		},
		calls: map[string]int{},
	}
	verifier := newTestPublicFormURLVerifier(t, resolver)

	for _, rawURL := range []string{
		"https://example.com/form",
		"http://Example.COM./form",
		"https://93.184.216.34/form",
		"https://[2606:2800:220:1:248:1893:25c8:1946]/form",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := verifier.VerifyFormURL(ctx, rawURL); err != nil {
				t.Fatalf("expected URL to be allowed: %v", err)
			}
		})
	}
}

func TestPublicFormURLVerifierCachesSafeHostnames(t *testing.T) {
	ctx := context.Background()
	resolver := &stubFormURLResolver{
		addresses: map[string][]string{
			"example.com": {"93.184.216.34"},
		},
		calls: map[string]int{},
	}
	verifier := newTestPublicFormURLVerifier(t, resolver)

	if err := verifier.VerifyFormURL(ctx, "https://example.com/form"); err != nil {
		t.Fatalf("expected first URL verification to pass: %v", err)
	}
	if err := verifier.VerifyFormURL(ctx, "https://Example.COM./other-form"); err != nil {
		t.Fatalf("expected cached URL verification to pass: %v", err)
	}
	if resolver.calls["example.com"] != 1 {
		t.Fatalf("expected one resolver call for cached hostname, got %d", resolver.calls["example.com"])
	}
}

func TestPublicFormURLVerifierBlocksUnsafeURLs(t *testing.T) {
	ctx := context.Background()
	dnsErr := errors.New("dns failed")
	resolver := &stubFormURLResolver{
		addresses: map[string][]string{
			"internal.example": {"10.0.0.1"},
			"empty.example":    {},
		},
		errors: map[string]error{
			"missing.example": dnsErr,
		},
		calls: map[string]int{},
	}
	verifier := newTestPublicFormURLVerifier(t, resolver)

	for _, rawURL := range []string{
		"http://localhost/form",
		"http://app.localhost/form",
		"http://printer.local/form",
		"http://127.0.0.1/form",
		"http://[::1]/form",
		"http://10.0.0.1/form",
		"http://172.16.0.1/form",
		"http://192.168.1.1/form",
		"http://[fd00::1]/form",
		"http://169.254.169.254/form",
		"http://[fe80::1]/form",
		"ftp://example.com/form",
		"http:///form",
		"http://user@example.com/form",
		"https://internal.example/form",
		"https://empty.example/form",
		"https://missing.example/form",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := verifier.VerifyFormURL(ctx, rawURL); err == nil {
				t.Fatal("expected URL to be blocked")
			}
		})
	}
}
