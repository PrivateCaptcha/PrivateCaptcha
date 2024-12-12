package email

import (
	"bytes"
	"context"
	"log/slog"
	"text/template"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type PortalMailer struct {
	mailer                *SimpleMailer
	Domain                string
	twofactorHTMLTemplate *template.Template
	twofactorTextTemplate *template.Template
}

func NewPortalMailer(domain string, getenv func(string) string) *PortalMailer {
	return &PortalMailer{
		mailer: &SimpleMailer{
			URL:      getenv("SMTP_ENDPOINT"),
			Username: getenv("SMTP_USERNAME"),
			Password: getenv("SMTP_PASSWORD"),
			Sender:   getenv("PC_EMAIL_FROM"),
		},
		Domain:                domain,
		twofactorHTMLTemplate: template.Must(template.New("HtmlBody").Parse(TwoFactorHTMLTemplate)),
		twofactorTextTemplate: template.Must(template.New("TextBody").Parse(twoFactorTextTemplate)),
	}
}

var _ common.Mailer = (*PortalMailer)(nil)

func (pm *PortalMailer) SendTwoFactor(ctx context.Context, email string, code int) error {
	data := struct {
		Code        int
		Domain      string
		CurrentYear int
	}{
		Code:        code,
		Domain:      pm.Domain,
		CurrentYear: time.Now().Year(),
	}

	var htmlBodyTpl bytes.Buffer
	if err := pm.twofactorHTMLTemplate.Execute(&htmlBodyTpl, data); err != nil {
		return err
	}

	var textBodyTpl bytes.Buffer
	if err := pm.twofactorTextTemplate.Execute(&textBodyTpl, data); err != nil {
		return err
	}

	msg := &Message{
		HTMLBody: htmlBodyTpl.String(),
		TextBody: textBodyTpl.String(),
		Subject:  "[Private Captcha] Your verification code",
		Email:    email,
		Campaign: "2fa",
		Track:    false,
	}
	err := pm.mailer.SendEmail(ctx, msg)
	slog.InfoContext(ctx, "Sent two factor code", "email", email, "code", code, common.ErrAttr(err))

	return err
}

func (pm *PortalMailer) SendSupportRequest(ctx context.Context, email string, category string, message string) error {
	// TODO: Implement sending support request email
	slog.InfoContext(ctx, "Sent support request", "category", category, "email", email)
	return nil
}
