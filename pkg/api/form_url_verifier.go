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
	errUnsafeFormURL = errors.New("unsafe form URL")
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

type FormURLSafetyCheckerImpl struct{}

func (*FormURLSafetyCheckerImpl) IsSafeFormHostname(host string) bool {
	return (host != "localhost") && !strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".local")
}

func (*FormURLSafetyCheckerImpl) IsSafeFormIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() &&
		ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsMulticast() &&
		!ip.IsUnspecified()
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
