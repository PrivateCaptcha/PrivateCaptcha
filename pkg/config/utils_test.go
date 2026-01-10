package config

import (
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestSplitHostPort(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		input         string
		expectedHost  string
		expectedPort  string
		expectedError bool
	}{
		{"empty_string", "", "", "", false},
		{"domain_only", "cdn.privatecaptcha.local", "cdn.privatecaptcha.local", "", false},
		{"domain_with_port", "cdn.privatecaptcha.local:8080", "cdn.privatecaptcha.local", "8080", false},
		{"ipv4_only", "192.168.1.1", "192.168.1.1", "", false},
		{"ipv4_with_port", "192.168.1.1:80", "192.168.1.1", "80", false},
		{"ipv6_with_port", "[::1]:8080", "::1", "8080", false},
		// net.SplitHostPort accepts localhost: with empty port
		{"trailing_colon", "localhost:", "localhost", "", false},
		// This is parsed as host="localhost", port="abc" by net.SplitHostPort
		{"alphabetic_port", "localhost:abc", "localhost", "abc", false},
		{"domain_colon_number", "example.com:443", "example.com", "443", false},
		{"localhost_with_port", "localhost:3000", "localhost", "3000", false},
		{"subdomain_with_port", "api.example.com:9000", "api.example.com", "9000", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			host, port, err := splitHostPort(tc.input)

			if tc.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none for input: %q", tc.input)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for input %q: %v", tc.input, err)
				return
			}

			if host != tc.expectedHost {
				t.Errorf("Host mismatch for %q: got %q, want %q", tc.input, host, tc.expectedHost)
			}

			if port != tc.expectedPort {
				t.Errorf("Port mismatch for %q: got %q, want %q", tc.input, port, tc.expectedPort)
			}
		})
	}
}

func TestAsInt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		value    string
		fallback int
		expected int
	}{
		{"valid_positive", "42", 0, 42},
		{"valid_negative", "-10", 0, -10},
		{"valid_zero", "0", 100, 0},
		{"empty_string_fallback", "", 99, 99},
		{"invalid_string_fallback", "abc", 50, 50},
		{"float_string_fallback", "3.14", 25, 25},
		{"mixed_chars_fallback", "123abc", 30, 30},
		{"whitespace_fallback", "  ", 10, 10},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			item := NewStaticValue(common.StageKey, tc.value)
			result := AsInt(item, tc.fallback)

			if result != tc.expected {
				t.Errorf("AsInt(%q, %d) = %d, want %d", tc.value, tc.fallback, result, tc.expected)
			}
		})
	}
}
