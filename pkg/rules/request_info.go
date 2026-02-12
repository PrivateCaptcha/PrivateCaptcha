package rules

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

// RequestInfo wraps http.Request and lazy-caches request attributes for rule matching
type RequestInfo struct {
	r                 *http.Request
	countryCodeHeader string

	userAgent   *string
	ipAddr      *netip.Addr
	countryCode *string
}

func NewRequestInfo(r *http.Request, countryCodeHeader string) *RequestInfo {
	return &RequestInfo{
		r:                 r,
		countryCodeHeader: countryCodeHeader,
	}
}

func (ri *RequestInfo) UserAgent() string {
	if ri.userAgent == nil {
		ua := ri.r.UserAgent()
		ri.userAgent = &ua
	}
	return *ri.userAgent
}

func (ri *RequestInfo) IPAddr() netip.Addr {
	if ri.ipAddr == nil {
		var addr netip.Addr
		if ip, ok := ri.r.Context().Value(common.RateLimitKeyContextKey).(netip.Addr); ok {
			addr = ip
		} else if host, _, err := net.SplitHostPort(ri.r.RemoteAddr); err == nil {
			if parsed, err := netip.ParseAddr(host); err == nil {
				addr = parsed
			}
		} else if parsed, err := netip.ParseAddr(ri.r.RemoteAddr); err == nil {
			addr = parsed
		}
		ri.ipAddr = &addr
	}
	return *ri.ipAddr
}

func (ri *RequestInfo) CountryCode() string {
	if ri.countryCode == nil {
		var cc string
		if len(ri.countryCodeHeader) > 0 {
			cc = ri.r.Header.Get(ri.countryCodeHeader)
		}
		ri.countryCode = &cc
	}
	return *ri.countryCode
}
