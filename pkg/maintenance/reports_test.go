package maintenance

import (
	"testing"
	"time"
)

func TestPercentChange(t *testing.T) {
	tests := []struct {
		name     string
		current  uint64
		previous uint64
		expected float64
	}{
		{"zero to zero", 0, 0, 0},
		{"zero to positive", 0, 100, -100},
		{"positive to zero", 100, 0, 100},
		{"increase", 150, 100, 50},
		{"decrease", 50, 100, -50},
		{"equal", 100, 100, 0},
		{"double", 200, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentChange(tt.current, tt.previous)
			if got != tt.expected {
				t.Errorf("percentChange(%d, %d) = %f, want %f", tt.current, tt.previous, got, tt.expected)
			}
		})
	}
}

func TestReferenceSuffix(t *testing.T) {
	if got := weeklyReferenceSuffix(2025, 11); got != "/2025/11" {
		t.Errorf("weeklyReferenceSuffix(2025, 11) = %q, want %q", got, "/2025/11")
	}
	if got := monthlyReferenceSuffix(2025, time.March); got != "/2025/3" {
		t.Errorf("monthlyReferenceSuffix(2025, March) = %q, want %q", got, "/2025/3")
	}
}

func TestTruncateDay(t *testing.T) {
	input := time.Date(2025, 3, 17, 14, 30, 45, 123, time.UTC)
	expected := time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC)
	if got := truncateDay(input); !got.Equal(expected) {
		t.Errorf("truncateDay(%v) = %v, want %v", input, got, expected)
	}
}
