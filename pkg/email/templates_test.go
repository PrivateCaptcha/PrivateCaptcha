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

func TestEmailTemplates(t *testing.T) {
	data := struct {
		OrgInvitationContext
		APIKeyExpirationContext
		TwoFactorEmailContext
		UsageReportContext
		// heap of everything else
		PortalURL   string
		CurrentYear int
		CDNURL      string
		UserName    string
	}{
		APIKeyExpirationContext: APIKeyExpirationContext{
			APIKeyContext: APIKeyContext{
				APIKeyName:         "My API Key",
				APIKeyPrefix:       db.APIKeyPrefix + "abcd",
				APIKeySettingsPath: "settings?tab=apikeys",
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
		},
		UserName:    "John Doe",
		CDNURL:      "https://cdn.privatecaptcha.com",
		PortalURL:   "https://portal.privatecaptcha.com",
		CurrentYear: time.Now().Year(),
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
