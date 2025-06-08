package portal

import (
	"fmt"
	"testing"
)

func TestRouteGenerator(t *testing.T) {
	testCases := []struct {
		parts    []string
		expected string
	}{
		{[]string{"login"}, "login"},
		{[]string{"org", "new"}, "org/new"},
		{[]string{"org", "1", "property", "1"}, "org/1/property/1"},
		{[]string{"org", "{org}", "property", "{prop}"}, "org/{org}/property/{prop}"},
	}

	rg := &RouteGenerator{
		Prefix: "privatecaptcha.com/",
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("route_path_%v", i), func(t *testing.T) {
			rg.Route("any", tc.parts...)

			if actual := rg.LastPath(); actual != tc.expected {
				t.Errorf("Actual path (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}
