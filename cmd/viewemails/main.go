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

const (
	stubPropertyURL = "https://portal.privatecaptcha.com/org/abc/property/123"
	stubFormURL     = "https://portal.privatecaptcha.com/org/abc/form/123"
	stubUTM         = "utm_medium=email&utm_source=dev"
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
	tpl, err := template.New("HtmlBody").Funcs(email.Functions()).Parse(templateBody)
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
		email.FormDeactivationContext
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
			Code:        "123456",
			Date:        time.Now().Format("02 Jan 2006 15:04:05 MST"),
			Browser:     fmt.Sprintf("%s %s", agent.Browser().String(), agent.BrowserVersion()),
			OS:          agent.OS().String(),
			Location:    "EE",
			ShowDetails: true,
		},
		UsageReportContext: email.UsageReportContext{
			Period:                 "weekly",
			PeriodDate:             time.Now().Format("02 Jan 2006"),
			TotalRequests:          1245000,
			TotalVerifies:          8320,
			PrevRequests:           11200,
			PrevVerifies:           9100,
			RequestsChange:         11.2,
			VerifiesChange:         -8.6,
			VerificationRateChange: -17.8,
			DashboardPath:          "settings?tab=usage",
			VerificationRate:       66.8,
			AccountLimit:           1000000,
			TotalFormSubmissions:   1140,
			TotalFormErrors:        124,
			PrevFormSubmissions:    970,
			PrevFormErrors:         140,
			FormSubmissionsChange:  17.5,
			FormErrorsChange:       -11.4,
			FormErrorRate:          10.9,
			FormErrorRateChange:    -24.1,
			TopProperties: []*email.PropertyStat{
				{Name: "Main Site with extremely long name", Domain: "*.example.com", Link: stubPropertyURL, Count: 5200, Percent: 41.8, Change: 8.3},
				{Name: "Blog", Domain: "blog.example.com", Link: stubPropertyURL, Count: 3100, Percent: 24.9, Change: 6.9, Alternate: true},
				{Name: "Shop", Domain: "shop.example.com", Link: stubPropertyURL, Count: 2050, Percent: 16.5, Change: -10.9},
				{Name: "Forum", Domain: "suddomain.app.forum.example.com", Link: stubPropertyURL, Count: 1300, Percent: 10.4, Change: 62.5, Alternate: true},
				{Name: "Docs", Domain: "docs.example.com", Link: stubPropertyURL, Count: 800, Percent: 6.4, Change: 100.0},
			},
			TopForms: []*email.FormStat{
				{Name: "Contact", URL: "https://hooks.example.com/contact", Link: stubFormURL, Count: 520, Percent: 45.6, Change: 11.2},
				{Name: "Support", URL: "https://hooks.example.com/support", Link: stubFormURL, Count: 380, Percent: 33.3, Change: -5.8, Alternate: true},
			},
			UTM: stubUTM,
		},
		FormDeactivationContext: email.FormDeactivationContext{
			Forms: []*email.DeactivatedForm{
				&email.DeactivatedForm{Name: "Contact us", Link: "http://localhost"},
				&email.DeactivatedForm{Name: "Support", Link: "http://localhost"},
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
