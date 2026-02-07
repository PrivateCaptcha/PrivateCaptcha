package common

import "net/netip"

const (
	ipv4MaskBits = 24
	ipv6MaskBits = 48
)

// MaskIPAddress masks an IP address for privacy by zeroing the last octet for IPv4
// or last 80 bits for IPv6. Returns the masked address.
func MaskIPAddress(ip netip.Addr) netip.Addr {
	if ip.Is4() {
		return netip.PrefixFrom(ip, ipv4MaskBits).Masked().Addr()
	}

	return netip.PrefixFrom(ip, ipv6MaskBits).Masked().Addr()
}
