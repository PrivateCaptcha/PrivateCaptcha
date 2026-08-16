package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

var (
	errUnsafeFormURL         = errors.New("unsafe form URL")
	formNAT64Prefix          = netip.MustParsePrefix("64:ff9b::/96")
	formNAT64LocalUsePrefix  = netip.MustParsePrefix("64:ff9b:1::/48")
	form6To4Prefix           = netip.MustParsePrefix("2002::/16")
	formIPv4CompatiblePrefix = netip.MustParsePrefix("::/96")
)

const (
	maxFormURLCacheSize = 10_000
)

type FormURLResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type FormURLSafetyChecker interface {
	IsSafeFormIP(ip netip.Addr) bool
	IsSafeFormHostname(host string) bool
}

type FormURLSafetyCheckerImpl struct {
	nat64Prefixes []netip.Prefix
}

func NewFormURLSafetyChecker(nat64Prefixes ...netip.Prefix) (*FormURLSafetyCheckerImpl, error) {
	for _, prefix := range nat64Prefixes {
		if !prefix.IsValid() || !prefix.Addr().Is6() || prefix != prefix.Masked() {
			return nil, fmt.Errorf("invalid RFC 6052 NAT64 prefix: %s", prefix)
		}
		if prefix.Addr().As16()[8] != 0 {
			return nil, fmt.Errorf("RFC 6052 NAT64 prefix has nonzero u octet: %s", prefix)
		}
		switch prefix.Bits() {
		case 32, 40, 48, 56, 64, 96:
		default:
			return nil, fmt.Errorf("unsupported RFC 6052 NAT64 prefix length: %s", prefix)
		}
	}

	return &FormURLSafetyCheckerImpl{
		nat64Prefixes: append([]netip.Prefix(nil), nat64Prefixes...),
	}, nil
}

func (*FormURLSafetyCheckerImpl) IsSafeFormHostname(host string) bool {
	return (host != "localhost") && !strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".local")
}

var unsafePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // RFC 6598 CGN
}

func isKnownUnsafeIP(ip netip.Addr) bool {
	for _, prefix := range unsafePrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func isSafeFormIP(ip netip.Addr) bool {
	return ip.IsValid() &&
		ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsMulticast() &&
		!ip.IsUnspecified() &&
		!isKnownUnsafeIP(ip)
}

func extractRFC6052IPv4(bytes [16]byte, prefixBits int) netip.Addr {
	switch prefixBits {
	case 32:
		return netip.AddrFrom4([4]byte{bytes[4], bytes[5], bytes[6], bytes[7]})
	case 40:
		return netip.AddrFrom4([4]byte{bytes[5], bytes[6], bytes[7], bytes[9]})
	case 48:
		return netip.AddrFrom4([4]byte{bytes[6], bytes[7], bytes[9], bytes[10]})
	case 56:
		return netip.AddrFrom4([4]byte{bytes[7], bytes[9], bytes[10], bytes[11]})
	case 64:
		return netip.AddrFrom4([4]byte{bytes[9], bytes[10], bytes[11], bytes[12]})
	case 96:
		return netip.AddrFrom4([4]byte{bytes[12], bytes[13], bytes[14], bytes[15]})
	default:
		return netip.Addr{}
	}
}

func extractEmbeddedIPv4(ip netip.Addr, nat64Prefixes []netip.Prefix) (netip.Addr, bool) {
	if !ip.Is6() {
		return netip.Addr{}, false
	}

	nat64Prefix := netip.Prefix{}
	if formNAT64Prefix.Contains(ip) {
		nat64Prefix = formNAT64Prefix
	}
	for _, prefix := range nat64Prefixes {
		if prefix.Contains(ip) && (!nat64Prefix.IsValid() || prefix.Bits() > nat64Prefix.Bits()) {
			nat64Prefix = prefix
		}
	}

	bytes := ip.As16()
	if nat64Prefix.IsValid() {
		if bytes[8] != 0 {
			return netip.Addr{}, true
		}
		return extractRFC6052IPv4(bytes, nat64Prefix.Bits()), true
	}

	switch {
	case formIPv4CompatiblePrefix.Contains(ip):
		return netip.AddrFrom4([4]byte{bytes[12], bytes[13], bytes[14], bytes[15]}), true
	case form6To4Prefix.Contains(ip):
		return netip.AddrFrom4([4]byte{bytes[2], bytes[3], bytes[4], bytes[5]}), true
	default:
		return netip.Addr{}, false
	}
}

func (c *FormURLSafetyCheckerImpl) IsSafeFormIP(ip netip.Addr) bool {
	if c == nil {
		return false
	}

	ip = ip.Unmap()
	if !isSafeFormIP(ip) {
		return false
	}
	if embedded, ok := extractEmbeddedIPv4(ip, c.nat64Prefixes); ok {
		return isSafeFormIP(embedded)
	}
	if formNAT64LocalUsePrefix.Contains(ip) {
		return false
	}

	return true
}

type FormURLVerifierImpl struct {
	Cache         common.Cache[string, *bool]
	Resolver      FormURLResolver
	SafetyChecker FormURLSafetyChecker
}

var _ common.FormURLVerifier = (*FormURLVerifierImpl)(nil)

func NewFormURLVerifier() *FormURLVerifierImpl {
	var cache common.Cache[string, *bool]
	var err error
	cache, err = db.NewMemoryCache[string, *bool]("form_url_verifier", maxFormURLCacheSize, nil, /*missing value*/
		10*time.Minute /*expiry*/, 5*time.Minute /*refresh*/, time.Minute /*missing */)
	if err != nil {
		slog.Error("Failed to create memory cache", common.ErrAttr(err))
		cache = db.NewStaticCache[string, *bool](maxFormURLCacheSize, nil)
	}

	return NewFormURLVerifierEx(cache, &FormURLSafetyCheckerImpl{}, net.DefaultResolver)
}

func NewFormURLVerifierEx(cache common.Cache[string, *bool], checker FormURLSafetyChecker, resolver FormURLResolver) *FormURLVerifierImpl {
	if cache == nil {
		cache = db.NewStaticCache[string, *bool](maxFormURLCacheSize, nil)
	}
	if checker == nil {
		checker = &FormURLSafetyCheckerImpl{}
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}

	return &FormURLVerifierImpl{
		Cache:         cache,
		Resolver:      resolver,
		SafetyChecker: checker,
	}
}

func (v *FormURLVerifierImpl) VerifyURL(ctx context.Context, rawURL string) error {
	if size := len(rawURL); (size == 0) || (size > db.MaxFormURLLength) {
		return fmt.Errorf("%w: length problem", errUnsafeFormURL)
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: malformed URL", errUnsafeFormURL)
	}

	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "https" {
		return fmt.Errorf("%w: unsupported scheme: %v", errUnsafeFormURL, scheme)
	}

	if parsedURL.User != nil {
		return fmt.Errorf("%w: userinfo is not allowed", errUnsafeFormURL)
	}

	host := normalizeFormURLHostname(parsedURL.Hostname())
	if len(host) == 0 {
		return fmt.Errorf("%w: missing hostname", errUnsafeFormURL)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return fmt.Errorf("%w: IP address hostname is not allowed", errUnsafeFormURL)
	}

	if verified, err := v.Cache.Get(ctx, host); (err == nil) && (verified != nil) {
		if *verified {
			return nil
		} else {
			return errUnsafeFormURL
		}
	}

	safe := new(bool)

	if !v.SafetyChecker.IsSafeFormHostname(host) {
		_ = v.Cache.Set(ctx, host, safe)
		return fmt.Errorf("%w: blocked hostname", errUnsafeFormURL)
	}

	resolver := v.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}

	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: DNS lookup failed: %v", errUnsafeFormURL, err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("%w: DNS lookup returned no addresses", errUnsafeFormURL)
	}

	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok || !v.SafetyChecker.IsSafeFormIP(ip) {
			_ = v.Cache.Set(ctx, host, safe)
			return fmt.Errorf("%w: DNS resolved unsafe address", errUnsafeFormURL)
		}
	}

	*safe = true
	_ = v.Cache.Set(ctx, host, safe)

	return nil
}

func (v *FormURLVerifierImpl) VerifyResolvedAddress(ctx context.Context, host string, ip netip.Addr) error {
	host = normalizeFormURLHostname(host)
	if len(host) == 0 {
		return fmt.Errorf("%w: missing hostname", errUnsafeFormURL)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return fmt.Errorf("%w: IP address hostname is not allowed", errUnsafeFormURL)
	}
	if !v.SafetyChecker.IsSafeFormHostname(host) {
		return fmt.Errorf("%w: blocked hostname", errUnsafeFormURL)
	}
	if !v.SafetyChecker.IsSafeFormIP(ip) {
		return fmt.Errorf("%w: blocked IP address", errUnsafeFormURL)
	}
	return nil
}

func normalizeFormURLHostname(host string) string {
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func (v *FormURLVerifierImpl) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	host = normalizeFormURLHostname(host)
	if ip, err := netip.ParseAddr(host); err == nil {
		if err := v.VerifyResolvedAddress(ctx, host, ip); err != nil {
			return nil, err
		}
		return formOutboundDialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}

	addresses, err := v.Resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("form dial hostname resolved no addresses: %s", host)
	}

	var lastErr error
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok {
			return nil, fmt.Errorf("form dial resolved invalid address: %s", address.IP.String())
		}
		if err := v.VerifyResolvedAddress(ctx, host, ip); err != nil {
			return nil, err
		}

		conn, err := formOutboundDialer.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

type AllowAllFormURLVerifier struct{}

var _ common.FormURLVerifier = (*AllowAllFormURLVerifier)(nil)

func (AllowAllFormURLVerifier) VerifyURL(ctx context.Context, rawURL string) error {
	return nil
}

func (AllowAllFormURLVerifier) VerifyResolvedAddress(ctx context.Context, host string, ip netip.Addr) error {
	return nil
}

func (AllowAllFormURLVerifier) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && (transport != nil) {
		return transport.DialContext(ctx, network, address)
	}

	panic("not configured")
}
