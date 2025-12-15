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
			//UserName:      "John Doe",
			OrgName:       "My Organization",
			OrgOwnerName:  "Pat Smith",
			OrgOwnerEmail: "john.doe@example.com",
			OrgURL:        "https://portal.privatecaptcha.com/org/5",
		},
		TwoFactorEmailContext: TwoFactorEmailContext{
			Code:     "123456",
			Date:     time.Now().Format("02 Jan 2006 15:04:05 MST"),
			Browser:  "Firefox",
			OS:       "Ubuntu",
			Location: "EE",
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
