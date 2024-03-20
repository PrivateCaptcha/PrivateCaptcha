package common

import (
	"fmt"
	"testing"
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
