package common

import (
	"net/netip"
	"testing"
)

func TestMaskedIPPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         netip.Addr
		masked        string
		wantFamily    uint8
		wantPrefix    uint64
		wantValidMask bool
	}{
		{
			name:          "IPv4",
			input:         netip.MustParseAddr("192.0.2.129"),
			masked:        "192.0.2.0",
			wantFamily:    4,
			wantPrefix:    0xc00002,
			wantValidMask: true,
		},
		{
			name:          "IPv6",
			input:         netip.MustParseAddr("2001:db8:abcd:1234::1"),
			masked:        "2001:db8:abcd::",
			wantFamily:    6,
			wantPrefix:    0x20010db8abcd,
			wantValidMask: true,
		},
		{
			name:          "IPv4MappedIPv6",
			input:         netip.MustParseAddr("::ffff:192.0.2.129"),
			masked:        "192.0.2.0",
			wantFamily:    4,
			wantPrefix:    0xc00002,
			wantValidMask: true,
		},
		{
			name:          "Unknown",
			wantFamily:    0,
			wantPrefix:    0,
			wantValidMask: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masked := MaskIPAddress(tt.input)
			if masked.IsValid() != tt.wantValidMask {
				t.Fatalf("MaskIPAddress(%v) valid = %v, want %v", tt.input, masked.IsValid(), tt.wantValidMask)
			}
			if tt.wantValidMask && masked.String() != tt.masked {
				t.Errorf("MaskIPAddress(%v) = %v, want %s", tt.input, masked, tt.masked)
			}

			family, prefix := MaskedIPPrefix(tt.input)
			if family != tt.wantFamily || prefix != tt.wantPrefix {
				t.Errorf("MaskedIPPrefix(%v) = (%d, %#x), want (%d, %#x)", tt.input, family, prefix, tt.wantFamily, tt.wantPrefix)
			}
		})
	}
}
