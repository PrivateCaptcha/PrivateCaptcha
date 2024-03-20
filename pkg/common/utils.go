package common

import "strings"

func RelURL(prefix, url string) string {
	url = strings.TrimPrefix(url, "/")
	p := strings.Trim(prefix, "/")
	if len(p) == 0 {
		return "/" + url
	}
	return "/" + p + "/" + url
}
