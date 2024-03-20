package common

import (
	"strings"
)

func RelURL(prefix, url string) string {
	url = strings.TrimPrefix(url, "/")
	p := strings.Trim(prefix, "/")
	if len(p) == 0 {
		return "/" + url
	}
	return "/" + p + "/" + url
}

func MaskEmail(email string, mask rune) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return email
	}

	username := parts[0]
	length := len(username)

	var keep int
	switch length {
	case 0, 1:
		keep = length
	case 2, 3:
		keep = 1
	case 4, 5:
		keep = 2
	case 6, 7:
		keep = 3
	case 8, 9:
		keep = 4
	default:
		keep = 5
	}

	prefix := username[:keep]
	suffix := ""

	n := length - keep
	if n > 5 {
		n = 5
		suffix = ".."
	}

	xxx := strings.Repeat(string(mask), n)

	return prefix + xxx + suffix + "@" + parts[1]
}
