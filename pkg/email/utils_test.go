package email

import (
	"testing"
)

func TestIsLikelyValidDomain(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		// Edge Case: Empty String
		{"Empty string", "", false},

		// Leading and Trailing Dots
		{"Leading dot", ".example.com", false},
		{"Trailing dot", "example.com.", false},

		// Whitespace Checks
		{"Contains space", "example .com", false},
		{"Contains tab", "example\t.com", false},
		{"Contains newline", "example\n.com", false},

		// Interior Dot Requirement
		{"No dots", "localhost", false},
		{"Only one dot (valid)", "example.com", true},

		// Consecutive Dots
		{"Consecutive dots", "example..com", false},
		{"Leading consecutive dots", "..example.com", false},

		// Success Cases
		{"Valid simple domain", "google.com", true},
		{"Valid subdomain", "blog.example.org", true},
		{"Valid multi-level subdomain", "dev.api.v1.example.io", true},
		{"Valid with hyphens", "my-cool-site.net", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsLikelyValidDomain(tc.input)
			if result != tc.expected {
				t.Errorf("IsLikelyValidDomain(%q) = %v; want %v", tc.input, result, tc.expected)
			}
		})
	}
}
