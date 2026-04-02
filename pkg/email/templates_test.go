package email

import (
	"fmt"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
)

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
			Code:     "123456",
			Date:     time.Now().Format("02 Jan 2006 15:04:05 MST"),
			Browser:  "Firefox",
			OS:       "Ubuntu",
			Location: "EE",
		},
		UsageReportContext: UsageReportContext{
			Period:           "weekly",
			TotalRequests:    1234,
			TotalVerifies:    567,
			PrevRequests:     1100,
			PrevVerifies:     500,
			RequestsChange:   12.2,
			VerifiesChange:   13.4,
			RequestsSign:     "+",
			VerifiesSign:     "+",
			RequestsColor:    "#16a34a",
			VerifiesColor:    "#16a34a",
			DashboardPath:    "settings?tab=usage",
			VerificationRate: 45.9,
			TopProperties: []PropertyStat{
				{Name: "Main Site", Domain: "example.com", Count: 800, Percent: 64.8, PrevCount: 700, Change: 14.3, ChangeSign: "+", ChangeColor: "#16a34a"},
				{Name: "Blog", Domain: "blog.example.com", Count: 434, Percent: 35.2, PrevCount: 400, Change: 8.5, ChangeSign: "+", ChangeColor: "#16a34a"},
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
