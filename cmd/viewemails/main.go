package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
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
	templates = map[string]string{}
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
		Code               int
		PortalURL          string
		CurrentYear        int
		CDNURL             string
		Message            string
		TicketID           string
		APIKeyName         string
		APIKeyPrefix       string
		ExpireDays         int
		APIKeySettingsPath string
		UserName           string
	}{
		UserName:           "John Doe",
		Code:               123456,
		CDNURL:             "https://cdn.privatecaptcha.com",
		PortalURL:          "https://portal.privatecaptcha.com",
		CurrentYear:        time.Now().Year(),
		Message:            "This is a support request message. Nothing works!",
		TicketID:           "qwerty12345",
		APIKeyName:         "My API Key",
		APIKeyPrefix:       "abcde",
		ExpireDays:         7,
		APIKeySettingsPath: "settings?tab=apikeys",
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

	for _, tpl := range email.Templates() {
		templates[tpl.Name()] = tpl.ContentHTML()
	}

	for k := range templates {
		http.HandleFunc("/"+k, serveTemplate(k))
	}

	log.Println("Listening at http://localhost:8082/")

	_ = http.ListenAndServe(":8082", nil)
}
