package email

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		n        int
		expected string
	}{
		{"shorter than n", "hi", 10, "hi"},
		{"equal to n", "hello", 5, "hello"},
		{"longer than n", "hello world", 8, "hello..."},
		{"n lte 3 short string fits", "ab", 3, "ab"},
		{"n lte 3 long string", "abcdef", 3, "..."},
		{"empty string", "", 5, ""},
		{"n zero empty string", "", 0, ""},
		{"unicode runes not split", "\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9", 6, "\u00e9\u00e9\u00e9..."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.input, tc.n)
			if got != tc.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.expected)
			}
		})
	}
}

func TestUsageReportTemplateFormStatsSection(t *testing.T) {
	ctx := t.Context()
	data := struct {
		UsageReportContext
		PortalURL   string
		CurrentYear int
		CDNURL      string
	}{
		UsageReportContext: UsageReportContext{
			Period:                 "weekly",
			PeriodDate:             time.Now().Format("02 Jan 2006"),
			TotalRequests:          100,
			TotalVerifies:          50,
			RequestsChange:         10,
			VerifiesChange:         5,
			VerificationRate:       50,
			VerificationRateChange: 2,
			DashboardPath:          "settings?tab=usage",
		},
		PortalURL:   "https://portal.privatecaptcha.com",
		CurrentYear: time.Now().Year(),
		CDNURL:      "https://cdn.privatecaptcha.com",
	}

	t.Run("HiddenWhenZero", func(t *testing.T) {
		html, err := UsageReportTemplate.RenderHTML(ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		text, err := UsageReportTemplate.RenderText(ctx, data)
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(html, "Total Submissions") {
			t.Fatal("did not expect form stats section in html when form totals are zero")
		}
		if strings.Contains(text, "Total Submissions") {
			t.Fatal("did not expect form stats section in text when form totals are zero")
		}
	})

	t.Run("ShownWhenPresent", func(t *testing.T) {
		data.TotalFormSubmissions = 20
		data.PrevFormSubmissions = 10
		data.TotalFormErrors = 4
		data.PrevFormErrors = 2
		data.FormSubmissionsChange = 100
		data.FormErrorsChange = 100
		data.FormErrorRate = 20
		data.FormErrorRateChange = 0

		html, err := UsageReportTemplate.RenderHTML(ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		text, err := UsageReportTemplate.RenderText(ctx, data)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(html, "Total Submissions") {
			t.Fatal("expected form stats section in html when form totals are present")
		}
		if !strings.Contains(text, "Total Submissions") {
			t.Fatal("expected form stats section in text when form totals are present")
		}
	})
}

func TestUsageReportTemplateTopFormsAfterProperties(t *testing.T) {
	ctx := t.Context()
	data := struct {
		UsageReportContext
		PortalURL   string
		CurrentYear int
		CDNURL      string
	}{
		UsageReportContext: UsageReportContext{
			Period:                 "weekly",
			PeriodDate:             time.Now().Format("02 Jan 2006"),
			TotalRequests:          200,
			TotalVerifies:          120,
			RequestsChange:         10,
			VerifiesChange:         5,
			VerificationRate:       60,
			VerificationRateChange: 2,
			DashboardPath:          "settings?tab=usage",
			TopProperties: []*PropertyStat{
				{Name: "Main Site", Domain: "example.com", Count: 150, Percent: 75, Change: 10},
			},
			TotalFormSubmissions: 20,
			TotalFormErrors:      4,
			TopForms: []*FormStat{
				{Name: "Contact", URL: "https://hooks.example.com/contact", Count: 20, Percent: 100, Change: 10},
			},
		},
		PortalURL:   "https://portal.privatecaptcha.com",
		CurrentYear: time.Now().Year(),
		CDNURL:      "https://cdn.privatecaptcha.com",
	}

	html, err := UsageReportTemplate.RenderHTML(ctx, data)
	if err != nil {
		t.Fatal(err)
	}

	propertiesIndex := strings.Index(html, "Top 1 properties by requests:")
	formsIndex := strings.Index(html, "Top 1 forms by submissions:")
	if propertiesIndex == -1 {
		t.Fatal("expected top properties section in html")
	}
	if formsIndex == -1 {
		t.Fatal("expected top forms section in html")
	}
	if formsIndex <= propertiesIndex {
		t.Fatal("expected top forms section after top properties section")
	}
}

func TestEmailTemplates(t *testing.T) {
	data := struct {
		OrgInvitationContext
		APIKeyExpirationContext
		TwoFactorEmailContext
		UsageReportContext
		FormDeactivationContext
		// heap of everything else
		PortalURL   string
		CurrentYear int
		CDNURL      string
		UserName    string
		UTM         string
	}{
		APIKeyExpirationContext: APIKeyExpirationContext{
			APIKeyContext: APIKeyContext{
				APIKeyName:         "My API Key",
				APIKeyPrefix:       db.APIKeyPrefix + "abcd",
				APIKeySettingsPath: "settings?tab=apikeys",
				UTM:                "utm_medium=email&utm_source=test",
			},
			ExpireDays: 7,
		},
		OrgInvitationContext: OrgInvitationContext{
			//UserName:         "John Doe",
			OrgName:          "My Organization",
			OrgOwnerName:     "Pat Smith",
			OrgOwnerEmail:    "john.doe@example.com",
			OrgURL:           "https://portal.privatecaptcha.com/org/5",
			RequiresRegister: false,
		},
		TwoFactorEmailContext: TwoFactorEmailContext{
			Code:        "123456",
			Date:        time.Now().Format("02 Jan 2006 15:04:05 MST"),
			Browser:     "Firefox",
			OS:          "Ubuntu",
			Location:    "EE",
			ShowDetails: true,
		},
		UsageReportContext: UsageReportContext{
			Period:                 "weekly",
			PeriodDate:             time.Now().Format("02 Jan 2006"),
			TotalRequests:          1234,
			TotalVerifies:          567,
			PrevRequests:           1100,
			PrevVerifies:           500,
			RequestsChange:         12.2,
			VerifiesChange:         13.4,
			VerificationRateChange: 1.0,
			DashboardPath:          "settings?tab=usage",
			VerificationRate:       45.9,
			TopProperties: []*PropertyStat{
				{Name: "Main Site", Domain: "example.com", Count: 800, Percent: 64.8, Change: 14.3},
				{Name: "Blog", Domain: "blog.example.com", Count: 434, Percent: 35.2, Change: 8.5, Alternate: true},
			},
			ProtectionHighlights: []*ProtectionHighlight{
				{Name: "Checkout", Link: "https://portal.privatecaptcha.com/org/abc/property/def", Date: "2026-08-14", Requests: 50, Verifies: 250},
			},
			TopForms: []*FormStat{
				{Name: "Contact", URL: "https://hooks.example.com/contact", Count: 80, Percent: 60.0, Change: 10.0},
				{Name: "Support", URL: "https://hooks.example.com/support", Count: 54, Percent: 40.0, Change: -5.0, Alternate: true},
			},
		},
		FormDeactivationContext: FormDeactivationContext{
			Forms: []*DeactivatedForm{
				{Name: "Contact", Link: "https://portal.privatecaptcha.com/org/abc/form/def"},
			},
		},
		UserName:    "John Doe",
		CDNURL:      "https://cdn.privatecaptcha.com",
		PortalURL:   "https://portal.privatecaptcha.com",
		CurrentYear: time.Now().Year(),
		UTM:         "utm_medium=email&utm_source=test",
	}

	for _, tpl := range templates {
		t.Run(fmt.Sprintf("emailTemplate_%v", tpl.Name()), func(t *testing.T) {
			ctx := t.Context()

			if _, err := tpl.RenderHTML(ctx, data); err != nil {
				t.Fatal(err)
			}

			if _, err := tpl.RenderText(ctx, data); err != nil {
				t.Fatal(err)
			}
		})
	}
}
