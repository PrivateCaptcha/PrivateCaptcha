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
			random: []byte{0, 10, 1, 11, 2, 35},
			want:   "0A1B2Z",
		},
		{
			name:   "Digits",
			random: []byte{0, 1, 2, 3, 4, 5},
			want:   "012345",
		},
		{
			name:   "Letters",
			random: []byte{10, 11, 12, 33, 34, 35},
			want:   "ABCXYZ",
		},
		{
			name:   "HighestAcceptedByte",
			random: bytes.Repeat([]byte{251}, 6),
			want:   "ZZZZZZ",
		},
		{
			name:   "UpperUniformRange",
			random: []byte{216, 217, 218, 219, 220, 221},
			want:   "012345",
		},
		{
			name:   "RejectBiasedBytes",
			random: []byte{252, 253, 254, 255, 0, 10, 1, 11, 2, 35},
			want:   "0A1B2Z",
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
