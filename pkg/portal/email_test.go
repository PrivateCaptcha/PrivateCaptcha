package portal

import (
	"context"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	emailpkg "github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

type captureSender struct {
	message *emailpkg.Message
}

func (s *captureSender) SendEmail(_ context.Context, message *emailpkg.Message) error {
	s.message = message
	return nil
}

type mailConfig struct{}

func (mailConfig) Get(key common.ConfigKey) common.ConfigItem {
	return config.NewStaticValue(key, "")
}

func (mailConfig) Update(context.Context) {}

func TestTwoFactorEmailValidatesLocation(t *testing.T) {
	tests := []struct {
		name          string
		location      string
		wantHTML      string
		wantText      string
		forbiddenText string
	}{
		{name: "uppercase country code", location: "US", wantHTML: ">US</td>", wantText: "Location: US"},
		{name: "lowercase country code", location: "de", wantHTML: ">DE</td>", wantText: "Location: DE"},
		{name: "arbitrary text", location: "approve at attacker.example", forbiddenText: "attacker.example"},
		{name: "oversized value", location: strings.Repeat("A", 8192), forbiddenText: strings.Repeat("A", 100)},
		{name: "non-letter code", location: "1A", forbiddenText: "Location:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &captureSender{}
			mailer := NewPortalMailer("https://cdn.example", "https://portal.example", sender, mailConfig{}, nil)

			if err := mailer.SendTwoFactor(t.Context(), "user@example.com", 123456, "", tt.location, false); err != nil {
				t.Fatal(err)
			}

			if (tt.wantHTML != "") && !strings.Contains(sender.message.HTMLBody, tt.wantHTML) {
				t.Errorf("HTML email body does not contain %q", tt.wantHTML)
			}
			if (tt.wantText != "") && !strings.Contains(sender.message.TextBody, tt.wantText) {
				t.Errorf("text email body does not contain %q", tt.wantText)
			}

			for _, body := range []string{sender.message.HTMLBody, sender.message.TextBody} {
				if (tt.forbiddenText != "") && strings.Contains(body, tt.forbiddenText) {
					t.Errorf("email body contains rejected location %q", tt.forbiddenText)
				}
			}
		})
	}
}
