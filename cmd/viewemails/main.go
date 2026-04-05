package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/medama-io/go-useragent"
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

type emailTemplateView struct {
	TemplateID  string
	ContentHTML string
}

var (
	templates = loadTemplates()
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
		_, _ = fmt.Fprintf(w, "<li><a href=\"/%s\">%s</a> <code>%s</code></li>\n", k, k, templates[k].TemplateID)
	}
	_, _ = w.Write([]byte(rootTemplateEnd))
}

func serveExecute(templateBody string, r *http.Request, w http.ResponseWriter) error {
	tpl, err := template.New("HtmlBody").Parse(templateBody)
	if err != nil {
		log.Printf("Failed to parse template: %v", err)
		return err
	}

	ua := useragent.NewParser()
	agent := ua.Parse(r.UserAgent())

	data := struct {
		email.OrgInvitationContext
		email.APIKeyExpirationContext
		email.TwoFactorEmailContext
		email.UsageReportContext
		// heap of everything else
		PortalURL   string
		CurrentYear int
		CDNURL      string
		UserName    string
	}{
		APIKeyExpirationContext: email.APIKeyExpirationContext{
			APIKeyContext: email.APIKeyContext{
				APIKeyName:         "My API Key",
				APIKeyPrefix:       db.APIKeyPrefix + "abcd",
				APIKeySettingsPath: "settings?tab=apikeys",
			},
			ExpireDays: 7,
		},
		OrgInvitationContext: email.OrgInvitationContext{
			//UserName:      "John Doe",
			OrgName:       "My Organization",
			OrgOwnerName:  "Pat Smith",
			OrgOwnerEmail: "john.doe@example.com",
			OrgURL:        "https://portal.privatecaptcha.com/org/5",
		},
		TwoFactorEmailContext: email.TwoFactorEmailContext{
			Code:     "123456",
			Date:     time.Now().Format("02 Jan 2006 15:04:05 MST"),
			Browser:  fmt.Sprintf("%s %s", agent.Browser().String(), agent.BrowserVersion()),
			OS:       agent.OS().String(),
			Location: "EE",
		},
		UsageReportContext: email.UsageReportContext{
			Period:                 "weekly",
			TotalRequests:          12450,
			TotalVerifies:          8320,
			PrevRequests:           11200,
			PrevVerifies:           9100,
			RequestsChange:         11.2,
			VerifiesChange:         -8.6,
			VerificationRateChange: -17.8,
			DashboardPath:          "settings?tab=usage",
			VerificationRate:       66.8,
			TopProperties: []email.PropertyStat{
				{Name: "Main Site", Domain: "example.com", Count: 5200, Percent: 41.8, Change: 8.3},
				{Name: "Blog", Domain: "blog.example.com", Count: 3100, Percent: 24.9, Change: 6.9, Alternate: true},
				{Name: "Shop", Domain: "shop.example.com", Count: 2050, Percent: 16.5, Change: -10.9},
				{Name: "Forum", Domain: "forum.example.com", Count: 1300, Percent: 10.4, Change: 62.5, Alternate: true},
				{Name: "Docs", Domain: "docs.example.com", Count: 800, Percent: 6.4, Change: 100.0},
			},
		},
		UserName:    "John Doe",
		CDNURL:      "https://cdn.privatecaptcha.com",
		PortalURL:   "https://portal.privatecaptcha.com",
		CurrentYear: time.Now().Year(),
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
			_, _ = w.Write([]byte(templates[name].ContentHTML))
			return
		}

		if err := serveExecute(templates[name].ContentHTML, r, w); err != nil {
			_, _ = w.Write([]byte(templates[name].ContentHTML))
		}
	}
}

func loadTemplates() map[string]emailTemplateView {
	result := make(map[string]emailTemplateView, len(email.Templates()))

	for _, tpl := range email.Templates() {
		result[tpl.Name()] = emailTemplateView{
			TemplateID:  tpl.Hash(),
			ContentHTML: tpl.ContentHTML(),
		}
	}

	return result
}

func main() {
	http.HandleFunc("/", homepage)

	for k := range templates {
		http.HandleFunc("/"+k, serveTemplate(k))
	}

	log.Println("Listening at http://localhost:8082/")

	_ = http.ListenAndServe(":8082", nil)
}
