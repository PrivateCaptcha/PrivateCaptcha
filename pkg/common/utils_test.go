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
