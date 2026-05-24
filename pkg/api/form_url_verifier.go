package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errUnsafeFormURL = errors.New("unsafe form URL")
)

type formURLResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type PublicFormURLVerifier struct {
	VerifiedHosts common.Cache[string, bool]
	Resolver      formURLResolver
}

var _ common.FormURLVerifier = (*PublicFormURLVerifier)(nil)

func NewPublicFormURLVerifier(verifiedHosts common.Cache[string, bool]) *PublicFormURLVerifier {
	return &PublicFormURLVerifier{
		VerifiedHosts: verifiedHosts,
		Resolver:      net.DefaultResolver,
	}
}

func (v *PublicFormURLVerifier) VerifyFormURL(ctx context.Context, rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: malformed URL", errUnsafeFormURL)
	}

	scheme := strings.ToLower(parsedURL.Scheme)
	if (scheme != "http") && (scheme != "https") {
		return fmt.Errorf("%w: unsupported scheme", errUnsafeFormURL)
	}

	if parsedURL.User != nil {
		return fmt.Errorf("%w: userinfo is not allowed", errUnsafeFormURL)
	}

	host := normalizeFormURLHostname(parsedURL.Hostname())
	if len(host) == 0 {
		return fmt.Errorf("%w: missing hostname", errUnsafeFormURL)
	}

	if isBlockedFormURLHostname(host) {
		return fmt.Errorf("%w: blocked hostname", errUnsafeFormURL)
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		if !isSafeFormURLIP(ip) {
			return fmt.Errorf("%w: blocked IP address", errUnsafeFormURL)
		}
		return nil
	}

	if v.VerifiedHosts != nil {
		if verified, err := v.VerifiedHosts.Get(ctx, host); err == nil && verified {
			return nil
		}
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
		if !ok || !isSafeFormURLIP(ip) {
			return fmt.Errorf("%w: DNS resolved unsafe address", errUnsafeFormURL)
		}
	}

	if v.VerifiedHosts != nil {
		if err := v.VerifiedHosts.Set(ctx, host, true); err != nil {
			return err
		}
	}

	return nil
}

func normalizeFormURLHostname(host string) string {
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func isBlockedFormURLHostname(host string) bool {
	return (host == "localhost") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local")
}

func isSafeFormURLIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() &&
		ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsMulticast() &&
		!ip.IsUnspecified()
}
