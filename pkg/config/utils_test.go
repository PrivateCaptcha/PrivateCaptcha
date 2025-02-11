package config

import (
	"fmt"
	"testing"
)

func TestSplitHost(t *testing.T) {
	testCases := []struct {
		value string
		host  string
	}{
		{"cdn.privatecaptcha.local", "cdn.privatecaptcha.local"},
		{"cdn.privatecaptcha.local:8080", "cdn.privatecaptcha.local"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("splitHost_%v", i), func(t *testing.T) {
			h, _, err := splitHostPort(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if h != tc.host {
				t.Errorf("Actual host (%v) is different from expected (%v)", h, tc.host)
			}
		})
	}
}
