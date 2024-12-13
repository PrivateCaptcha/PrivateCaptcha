package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

const (
	rootTemplateStart = `
<html>
<body>
<strong>Templates:</strong>
<ul>
`
	rootTemplateEnd = `</ul>
</body>
</html>`
)

var (
	templates = map[string]string{
		"two-factor": email.TwoFactorHTMLTemplate,
		"support":    email.SupportHTMLTemplate,
	}
)

func homepage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(rootTemplateStart))

	keys := make([]string, 0, len(templates))
	for k := range templates {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "<li><a href=\"/%s\">%s</a></li>\n", k, k)
	}
	_, _ = w.Write([]byte(rootTemplateEnd))
}

func serveTemplate(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(templates[name]))
	}
}

func main() {
	http.HandleFunc("/", homepage)

	for k := range templates {
		http.HandleFunc("/"+k, serveTemplate(k))
	}

	log.Println("Listening at http://localhost:8082/")

	_ = http.ListenAndServe(":8082", nil)
}
