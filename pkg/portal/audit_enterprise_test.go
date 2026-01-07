//go:build enterprise

package portal

import (
	"context"
	"testing"
)

func TestAuditLogsDaysFromParam(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		input    string
		expected int
	}{
		{"14", 14},
		{"30", 30},
		{"90", 90},
		{"180", 180},
		{"365", 365},
		{"", 14},
		{"invalid", 14},
		{"7", 14},
		{"100", 14},
		{"-1", 14},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := auditLogsDaysFromParam(ctx, tc.input)
			if result != tc.expected {
				t.Errorf("auditLogsDaysFromParam(%q) = %d, want %d", tc.input, result, tc.expected)
			}
		})
	}
}

func TestMaxAuditLogsForDays(t *testing.T) {
	tests := []struct {
		days     int
		expected int
	}{
		{14, 1400},
		{30, 3000},
		{90, 9000},
		{180, 18000},
		{365, 36500},
	}

	for _, tc := range tests {
		result := maxAuditLogsForDays(tc.days)
		if result != tc.expected {
			t.Errorf("maxAuditLogsForDays(%d) = %d, want %d", tc.days, result, tc.expected)
		}
	}
}
