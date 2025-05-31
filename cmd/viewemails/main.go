package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"sort"
	"text/template"
	"time"

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
		"welcome":    email.WelcomeHTMLTemplate,
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

func serveExecute(templateBody string, w http.ResponseWriter) error {
	tpl, err := template.New("HtmlBody").Parse(templateBody)
	if err != nil {
		log.Printf("Failed to parse template: %v", err)
		return err
	}

	data := struct {
		Code        int
		Domain      string
		CurrentYear int
		CDN         string
		Message     string
		TicketID    string
	}{
		Code:        123456,
		CDN:         "https://cdn.staging.privatecaptcha.com",
		Domain:      "https://staging.privatecaptcha.com",
		CurrentYear: time.Now().Year(),
		Message:     "This is a support request message. Nothing works!",
		TicketID:    "qwerty12345",
	}

	var htmlBodyTpl bytes.Buffer
	if err := tpl.Execute(&htmlBodyTpl, data); err != nil {
		log.Printf("Failed to execute template: %v", err)
		return err
	}

	htmlBodyTpl.WriteTo(w)

	return nil
}

func serveTemplate(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		mode := r.URL.Query().Get("mode")
		if mode == "raw" {
			_, _ = w.Write([]byte(templates[name]))
			return
		}

		if err := serveExecute(templates[name], w); err != nil {
			_, _ = w.Write([]byte(templates[name]))
		}
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
