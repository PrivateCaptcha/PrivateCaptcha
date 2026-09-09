package common

import "net/netip"

const (
	ipv4MaskBits = 24
	ipv6MaskBits = 48
)

// MaskIPAddress masks an IP address for privacy by zeroing the last octet for IPv4
// or last 80 bits for IPv6. Returns the masked address.
func MaskIPAddress(ip netip.Addr) netip.Addr {
	if !ip.IsValid() {
		return netip.Addr{}
	}

	ip = ip.Unmap()
	if ip.Is4() {
		return netip.PrefixFrom(ip, ipv4MaskBits).Masked().Addr()
	}

	return netip.PrefixFrom(ip, ipv6MaskBits).Masked().Addr()
}

// MaskedIPPrefix returns a masked IP family and network prefix for analytics.
func MaskedIPPrefix(ip netip.Addr) (uint8, uint64) {
	masked := MaskIPAddress(ip)
	if !masked.IsValid() {
		return 0, 0
	}

	if masked.Is4() {
		bytes := masked.As4()
		return 4, uint64(bytes[0])<<16 | uint64(bytes[1])<<8 | uint64(bytes[2])
	}

	bytes := masked.As16()
	return 6, uint64(bytes[0])<<40 | uint64(bytes[1])<<32 | uint64(bytes[2])<<24 |
		uint64(bytes[3])<<16 | uint64(bytes[4])<<8 | uint64(bytes[5])
}
