package api

import (
	"context"
	"errors"
	"net"
	"net/netip"
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

type stubFormURLSafetyChecker struct {
	hosts     map[string]bool
	ips       map[string]bool
	hostCalls []string
	ipCalls   []string
}

func (c *stubFormURLSafetyChecker) IsSafeFormHostname(host string) bool {
	c.hostCalls = append(c.hostCalls, host)
	if c.hosts == nil {
		return true
	}
	allowed, ok := c.hosts[host]
	return !ok || allowed
}

func (c *stubFormURLSafetyChecker) IsSafeFormIP(ip netip.Addr) bool {
	canonicalIP := ip.Unmap().String()
	c.ipCalls = append(c.ipCalls, canonicalIP)
	if c.ips == nil {
		return true
	}
	allowed, ok := c.ips[canonicalIP]
	return !ok || allowed
}

func newTestFormURLVerifier(t *testing.T) *FormURLVerifierImpl {
	t.Helper()

	return newTestFormURLVerifierEx(t, &stubFormURLSafetyChecker{}, &stubFormURLResolver{})
}

func newTestFormURLVerifierEx(t *testing.T, checker FormURLSafetyChecker, resolver FormURLResolver) *FormURLVerifierImpl {
	t.Helper()

	cache, err := db.NewMemoryCache[string, *bool]("test-form-url-verifier", 1000, nil, time.Minute, time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	return NewFormURLVerifierEx(cache, checker, resolver)
}

func TestPublicFormURLVerifierAllowsPublicURLs(t *testing.T) {
	ctx := context.Background()
	resolver := &stubFormURLResolver{
		addresses: map[string][]string{
			"example.com": {"93.184.216.34"},
		},
		calls: map[string]int{},
	}
	verifier := newTestFormURLVerifierEx(t, &stubFormURLSafetyChecker{}, resolver)

	for _, rawURL := range []string{
		"https://example.com/form",
		"https://Example.COM./form",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := verifier.VerifyURL(ctx, rawURL); err != nil {
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
	verifier := newTestFormURLVerifierEx(t, &stubFormURLSafetyChecker{}, resolver)

	if err := verifier.VerifyURL(ctx, "https://example.com/form"); err != nil {
		t.Fatalf("expected first URL verification to pass: %v", err)
	}
	if err := verifier.VerifyURL(ctx, "https://Example.COM./other-form"); err != nil {
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
	verifier := newTestFormURLVerifierEx(t, &FormURLSafetyCheckerImpl{}, resolver)

	for _, rawURL := range []string{
		"https://127.0.0.1/form",
		"https://[::1]/form",
		"https://93.184.216.34/form",
		"https://[2606:2800:220:1:248:1893:25c8:1946]/form",
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
			if err := verifier.VerifyURL(ctx, rawURL); err == nil {
				t.Fatal("expected URL to be blocked")
			}
		})
	}
}

func TestFormURLVerifierUsesInjectedSafetyChecker(t *testing.T) {
	ctx := context.Background()
	resolver := &stubFormURLResolver{
		addresses: map[string][]string{
			"example.com": {"93.184.216.34"},
		},
		calls: map[string]int{},
	}
	cache, err := db.NewMemoryCache[string, *bool]("test-form-url-verifier-injected", 1000, nil, time.Minute, time.Minute, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checker := &stubFormURLSafetyChecker{
		hosts: map[string]bool{"example.com": true},
		ips:   map[string]bool{"93.184.216.34": false},
	}
	verifier := NewFormURLVerifierEx(cache, checker, resolver)

	err = verifier.VerifyURL(ctx, "https://example.com/form")
	if err == nil {
		t.Fatal("expected injected checker to block resolved IP")
	}
	if len(checker.hostCalls) != 1 || checker.hostCalls[0] != "example.com" {
		t.Fatalf("expected hostname safety check for example.com, got %v", checker.hostCalls)
	}
	if len(checker.ipCalls) != 1 || checker.ipCalls[0] != "93.184.216.34" {
		t.Fatalf("expected IP safety check for 93.184.216.34, got %v", checker.ipCalls)
	}
}

func TestFormURLSafetyCheckerImplMethods(t *testing.T) {
	checker := &FormURLSafetyCheckerImpl{}

	if checker.IsSafeFormHostname("localhost") {
		t.Fatal("expected localhost to be unsafe")
	}
	if !checker.IsSafeFormHostname("example.com") {
		t.Fatal("expected example.com to be safe")
	}
	if checker.IsSafeFormIP(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("expected loopback IP to be unsafe")
	}
	if checker.IsSafeFormIP(netip.MustParseAddr("::1")) {
		t.Fatal("expected IPv6 loopback IP to be unsafe")
	}
	if checker.IsSafeFormIP(netip.MustParseAddr("::ffff:127.0.0.1")) {
		t.Fatal("expected IPv4-mapped loopback IP to be unsafe")
	}
	if checker.IsSafeFormIP(netip.MustParseAddr("::ffff:10.0.0.1")) {
		t.Fatal("expected IPv4-mapped private IP to be unsafe")
	}
	if checker.IsSafeFormIP(netip.Addr{}) {
		t.Fatal("expected invalid IP to be unsafe")
	}
	if !checker.IsSafeFormIP(netip.MustParseAddr("93.184.216.34")) {
		t.Fatal("expected public IP to be safe")
	}
	if !checker.IsSafeFormIP(netip.MustParseAddr("::ffff:93.184.216.34")) {
		t.Fatal("expected IPv4-mapped public IP to be safe")
	}
}

func TestFormURLVerifierRejectsDNSResolvedIPv6Loopback(t *testing.T) {
	resolver := &stubFormURLResolver{
		addresses: map[string][]string{"example.com": {"::1"}},
		calls:     map[string]int{},
	}
	verifier := newTestFormURLVerifierEx(t, &FormURLSafetyCheckerImpl{}, resolver)

	if err := verifier.VerifyURL(context.Background(), "https://example.com/form"); !errors.Is(err, errUnsafeFormURL) {
		t.Fatalf("expected DNS-resolved IPv6 loopback to be rejected as unsafe, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifier.DialContext(ctx, "tcp", "example.com:443"); !errors.Is(err, errUnsafeFormURL) {
		t.Fatalf("expected DNS-resolved IPv6 loopback dial to be rejected as unsafe, got %v", err)
	}
}

func TestFormURLSafetyCheckerRejectsUnsafeIPv6TransitionAddresses(t *testing.T) {
	checker := &FormURLSafetyCheckerImpl{}

	for _, tc := range []struct {
		name string
		ip   string
	}{
		{name: "NAT64 private", ip: "64:ff9b::10.0.0.1"},
		{name: "NAT64 loopback", ip: "64:ff9b::127.0.0.1"},
		{name: "NAT64 link local", ip: "64:ff9b::169.254.169.254"},
		{name: "6to4 private", ip: "2002:0a00:0001::"},
		{name: "6to4 loopback", ip: "2002:7f00:0001::"},
		{name: "6to4 link local", ip: "2002:a9fe:a9fe::"},
		{name: "IPv4 compatible private", ip: "::10.0.0.1"},
		{name: "IPv4 compatible loopback", ip: "::127.0.0.1"},
		{name: "IPv4 compatible link local", ip: "::169.254.169.254"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if checker.IsSafeFormIP(netip.MustParseAddr(tc.ip)) {
				t.Fatalf("expected IPv6 transition address %s to be unsafe", tc.ip)
			}
		})
	}
}

func TestFormURLSafetyCheckerAllowsPublicIPv6TransitionAddresses(t *testing.T) {
	checker := &FormURLSafetyCheckerImpl{}

	for _, ip := range []string{
		"64:ff9b::93.184.216.34",
		"2002:5db8:d822::",
		"::93.184.216.34",
	} {
		t.Run(ip, func(t *testing.T) {
			if !checker.IsSafeFormIP(netip.MustParseAddr(ip)) {
				t.Fatalf("expected IPv6 transition address %s to be safe", ip)
			}
		})
	}
}

func TestFormURLDialContextRejectsUnsafeIPv6TransitionAddresses(t *testing.T) {
	for _, ip := range []string{
		"64:ff9b::10.0.0.1",
		"2002:0a00:0001::",
		"::10.0.0.1",
	} {
		t.Run(ip, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			resolver := &stubFormURLResolver{
				addresses: map[string][]string{"example.com": {ip}},
				calls:     map[string]int{},
			}
			verifier := newTestFormURLVerifierEx(t, &FormURLSafetyCheckerImpl{}, resolver)

			_, err := verifier.DialContext(ctx, "tcp", "example.com:443")
			if !errors.Is(err, errUnsafeFormURL) {
				t.Fatalf("expected IPv6 transition address %s to be rejected as unsafe, got %v", ip, err)
			}
		})
	}
}
