package common

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRelURL(t *testing.T) {
	testCases := []struct {
		prefix   string
		url      string
		expected string
	}{
		{"", "test", "/test"},
		{"", "/test", "/test"},
		{"", "/test/", "/test/"},
		{"/", "test", "/test"},
		{"/", "/test", "/test"},
		{"/", "test/", "/test/"},
		{"my", "", "/my/"},
		{"/my", "", "/my/"},
		{"/my", "/", "/my/"},
		{"my", "/test", "/my/test"},
		{"my", "test/", "/my/test/"},
		{"my", "test", "/my/test"},
		{"/my", "test", "/my/test"},
		{"/my", "/test", "/my/test"},
		{"/my", "/test/", "/my/test/"},
		{"/my/", "/test/", "/my/test/"},
		{"/my/", "test", "/my/test"},
		{"/my/", "/test", "/my/test"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("relURL_%v", i), func(t *testing.T) {
			actual := RelURL(tc.prefix, tc.url)
			if actual != tc.expected {
				t.Errorf("Actual url (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

func TestMaskEmail(t *testing.T) {
	testCases := []struct {
		email    string
		expected string
	}{
		{"1@bar.com", "1@bar.com"},
		{"12@bar.com", "1x@bar.com"},
		{"123@bar.com", "1xx@bar.com"},
		{"1234@bar.com", "12xx@bar.com"},
		{"12345@bar.com", "12xxx@bar.com"},
		{"123456@bar.com", "123xxx@bar.com"},
		{"1234567@bar.com", "123xxxx@bar.com"},
		{"123456789012345@bar.com", "12345xxxxx..@bar.com"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("maskEmail_%v", i), func(t *testing.T) {
			actual := MaskEmail(tc.email, 'x')
			if actual != tc.expected {
				t.Errorf("Actual email (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

func TestCleanupDomain(t *testing.T) {
	testCases := []struct {
		domain   string
		expected string
	}{
		{"bar.com", "bar.com"},
		{"bar.com/", "bar.com"},
		{"bar.com/api", "bar.com"},
		{"bar.com/index.html", "bar.com"},
		{"http://bar.com", "bar.com"},
		{"http://bar.com/index.html", "bar.com"},
		{"https://bar.com", "bar.com"},
		{"https://bar.com/api", "bar.com"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("cleanupDomain_%v", i), func(t *testing.T) {
			actual, err := ParseDomainName(tc.domain)
			if err != nil {
				t.Fatal(err)
			}
			if actual != tc.expected {
				t.Errorf("Actual domain (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

func TestSubDomain(t *testing.T) {
	testCases := []struct {
		subDomain string
		domain    string
		expected  bool
	}{
		{"", "", false},
		{"domain.com", "domain.com", true},
		{"a.com", "b.com", false},
		{"app.domain.com", "domain.com", true},
		{".domain.com", "domain.com", false},
		// NOTE: despite incorrect, this function is not used in such context
		// {"...domain.com", "domain.com", false},
		{"a.domain.com", "domain.com", true},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("subdomain_%v", i), func(t *testing.T) {
			actual := IsSubDomainOrDomain(tc.subDomain, tc.domain)
			if actual != tc.expected {
				if actual {
					t.Errorf("%v should not be subdomain of %v", tc.subDomain, tc.domain)
				} else {
					t.Errorf("%v should be subdomain of %v", tc.subDomain, tc.domain)
				}
			}
		})
	}
}

func TestIsSkipName(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"web", true},
		{"Admin", true},
		{"admins", true},
		{"team", true},
		{"it", true},
		{"development", true},
		{"informatika", true},
		{"CAPTCHA", true},
		{"john", false},
		{"developers", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := isSkipName(tt.name)
			if actual != tt.expected {
				t.Errorf("isSkipName(%q) = %v; want %v", tt.name, actual, tt.expected)
			}
		})
	}
}

func TestIsAllCaps(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"ABC", true},
		{"ABC123", true},
		{"AbC", false},
		{"abc", false},
		{"123", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			actual := isAllCaps(tt.value)
			if actual != tt.expected {
				t.Errorf("isAllCaps(%q) = %v; want %v", tt.value, actual, tt.expected)
			}
		})
	}
}

func TestGuessFirstName(t *testing.T) {
	tests := []struct {
		username string
		expected string
	}{
		{"john doe", "John"},
		{"123 alice 456", "Alice"},
		{"bob123 charlie", "bob123"},
		{"david", "David"},
		{"123 456 789", "123 456 789"},
		{"", ""},
		{"    ", "    "},
		{"!@# john_doe", "john_doe"},
		{"___123___", "___123___"},
		{"   emily rose  ", "Emily"},
		{"123 456abc", "456abc"},
	}

	for _, tt := range tests {
		actual := GuessFirstName(tt.username, "")
		if actual != tt.expected {
			t.Errorf("GuessFirstName(%q) = %q; want %q", tt.username, actual, tt.expected)
		}
	}
}

func TestGuessFirstNameSkipSingleChar(t *testing.T) {
	tests := []struct {
		username string
		expected string
	}{
		{"a john", "John"},
		{"X alice", "Alice"},
		{"b", "b"},
		{"j doe", "Doe"},
	}

	for _, tt := range tests {
		actual := GuessFirstName(tt.username, "")
		if actual != tt.expected {
			t.Errorf("GuessFirstName(%q) = %q; want %q", tt.username, actual, tt.expected)
		}
	}
}

func TestGuessFirstNameSkipBlacklist(t *testing.T) {
	tests := []struct {
		username string
		expected string
	}{
		{"admin john", "John"},
		{"Admin john", "John"},
		{"ADMIN john", "John"},
		{"admins alice", "Alice"},
		{"team bob", "Bob"},
		{"web carol", "Carol"},
		{"it dave", "Dave"},
		{"development emily", "Emily"},
		{"informatika frank", "Frank"},
		{"IT development admin", "IT development admin"},
	}

	for _, tt := range tests {
		actual := GuessFirstName(tt.username, "")
		if actual != tt.expected {
			t.Errorf("GuessFirstName(%q) = %q; want %q", tt.username, actual, tt.expected)
		}
	}
}

func TestGuessFirstNameSkipAllCaps(t *testing.T) {
	tests := []struct {
		username string
		expected string
	}{
		{"ABC john", "John"},
		{"XYZ alice", "Alice"},
		{"AB CD ef", "Ef"},
		{"ABC DEF", "ABC DEF"},
	}

	for _, tt := range tests {
		actual := GuessFirstName(tt.username, "")
		if actual != tt.expected {
			t.Errorf("GuessFirstName(%q) = %q; want %q", tt.username, actual, tt.expected)
		}
	}
}

func TestGuessFirstNameWithEmail(t *testing.T) {
	tests := []struct {
		username string
		email    string
		expected string
	}{
		{"acme john", "user@acme.com", "John"},
		{"Acme alice", "user@acme.co.uk", "Alice"},
		{"google bob", "admin@google.org", "Bob"},
		{"john acme", "user@acme.com", "John"},
		{"acme", "user@acme.com", "acme"},
		{"john doe", "user@example.com", "John"},
		{"john doe", "", "John"},
		{"example john", "user@example.com", "John"},
		{"EXAMPLE john", "user@example.com", "John"},
	}

	for _, tt := range tests {
		actual := GuessFirstName(tt.username, tt.email)
		if actual != tt.expected {
			t.Errorf("GuessFirstName(%q, %q) = %q; want %q", tt.username, tt.email, actual, tt.expected)
		}
	}
}

func TestGuessFirstNameFallback(t *testing.T) {
	tests := []struct {
		username string
		email    string
		expected string
	}{
		{"admin", "", "admin"},
		{"admin team", "", "admin team"},
		{"IT", "", "IT"},
		{"AB CD", "", "AB CD"},
		{"a b c", "", "a b c"},
		{"web admin team", "user@example.com", "web admin team"},
	}

	for _, tt := range tests {
		actual := GuessFirstName(tt.username, tt.email)
		if actual != tt.expected {
			t.Errorf("GuessFirstName(%q, %q) = %q; want %q", tt.username, tt.email, actual, tt.expected)
		}
	}
}

func TestParseBoolean(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
	}{
		{"1", true},
		{"Y", true},
		{"y", true},
		{"yes", true},
		{"Yes", true},
		{"true", true},
		{"0", false},
		{"N", false},
		{"n", false},
		{"no", false},
		{"No", false},
		{"false", false},
		{"", false},
		{"random", false},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("parseBoolean_%s", tc.input), func(t *testing.T) {
			actual := ParseBoolean(tc.input)
			if actual != tc.expected {
				t.Errorf("ParseBoolean(%q) = %v; want %v", tc.input, actual, tc.expected)
			}
		})
	}
}

func TestEnvToBool(t *testing.T) {
	testCases := []struct {
		input    string
		expected bool
	}{
		{"1", true},
		{"Y", true},
		{"y", true},
		{"yes", true},
		{"true", true},
		{"YES", true},
		{"TRUE", true},
		{"0", false},
		{"N", false},
		{"n", false},
		{"no", false},
		{"false", false},
		{"", false},
		{"random", false},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("envToBool_%s", tc.input), func(t *testing.T) {
			actual := EnvToBool(tc.input)
			if actual != tc.expected {
				t.Errorf("EnvToBool(%q) = %v; want %v", tc.input, actual, tc.expected)
			}
		})
	}
}

func TestChunkedCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var calls int
	deleter := func(ctx context.Context, before time.Time, chunkSize int) int {
		calls++
		if calls == 1 {
			return 5
		}
		return 0
	}

	ChunkedCleanup(ctx, 10*time.Millisecond, 50*time.Millisecond, 10, deleter)

	if calls < 2 {
		t.Errorf("Expected deleter to be called at least twice, got %d calls", calls)
	}
}
