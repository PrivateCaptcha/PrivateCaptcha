package email

import (
	"strings"
)

func IsLikelyValidDomain(s string) bool {
	if len(s) == 0 {
		return false
	}

	// Disallow leading or trailing dots (e.g. ".com", "example.")
	if s[0] == '.' || s[len(s)-1] == '.' {
		return false
	}

	// Disallow whitespace in the domain
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}

	// Require at least one dot in the interior of the string
	if !strings.Contains(s, ".") {
		return false
	}

	// Disallow empty labels (e.g. "..invalid", "example..com")
	if strings.Contains(s, "..") {
		return false
	}

	// this is an actual email
	if strings.Contains(s, "@") {
		return false
	}

	return true
}
