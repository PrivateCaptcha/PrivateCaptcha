package portal

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestTwoFactorCode(t *testing.T) {
	tests := []struct {
		name   string
		random []byte
		want   string
	}{
		{
			name:   "LeadingZero",
			random: []byte{0, 10, 1, 11, 2, 28},
			want:   "0B1C2Z",
		},
		{
			name:   "Digits",
			random: []byte{0, 1, 2, 3, 4, 5},
			want:   "012345",
		},
		{
			name:   "Letters",
			random: []byte{10, 11, 12, 26, 27, 28},
			want:   "BCDWXZ",
		},
		{
			name:   "HighestAcceptedByte",
			random: bytes.Repeat([]byte{231}, 6),
			want:   "ZZZZZZ",
		},
		{
			name:   "UpperUniformRange",
			random: []byte{203, 204, 205, 206, 207, 208},
			want:   "012345",
		},
		{
			name:   "RejectBiasedBytes",
			random: []byte{232, 233, 254, 255, 0, 10, 1, 11, 2, 28},
			want:   "0B1C2Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := rand.Reader
			rand.Reader = bytes.NewReader(tt.random)
			t.Cleanup(func() {
				rand.Reader = original
			})
			if got := twoFactorCode(); got != tt.want {
				t.Fatalf("two-factor code = %q, want %q", got, tt.want)
			}
		})
	}
}
