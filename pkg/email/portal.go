package email

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type PortalMailer struct {
	mailer                *simpleMailer
	cdn                   string
	domain                string
	emailFrom             common.ConfigItem
	twofactorHTMLTemplate *template.Template
	twofactorTextTemplate *template.Template
	welcomeHTMLTemplate   *template.Template
	welcomeTextTemplate   *template.Template
}

func NewPortalMailer(cdn, domain string, mailer *simpleMailer, cfg common.ConfigStore) *PortalMailer {
	return &PortalMailer{
		mailer:                mailer,
		emailFrom:             cfg.Get(common.EmailFromKey),
		cdn:                   cdn,
		domain:                domain,
		twofactorHTMLTemplate: template.Must(template.New("HtmlBody").Parse(TwoFactorHTMLTemplate)),
		twofactorTextTemplate: template.Must(template.New("TextBody").Parse(twoFactorTextTemplate)),
		welcomeHTMLTemplate:   template.Must(template.New("HtmlBody").Parse(WelcomeHTMLTemplate)),
		welcomeTextTemplate:   template.Must(template.New("TextBody").Parse(welcomeTextTemplate)),
	}
}

var _ common.Mailer = (*PortalMailer)(nil)

func (pm *PortalMailer) SendTwoFactor(ctx context.Context, email string, code int) error {
	if len(email) == 0 {
		return errInvalidEmail
	}

	data := struct {
		Code        string
		Domain      string
		CurrentYear int
		CDN         string
	}{
		Code:        fmt.Sprintf("%06d", code),
		CDN:         pm.cdn,
		Domain:      fmt.Sprintf("https://%s/", pm.domain),
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
		HTMLBody:  htmlBodyTpl.String(),
		TextBody:  textBodyTpl.String(),
		Subject:   fmt.Sprintf("[%s] Your verification code is %v", common.PrivateCaptcha, data.Code),
		EmailTo:   email,
		EmailFrom: pm.emailFrom.Value(),
		NameFrom:  common.PrivateCaptcha,
	}

	clog := slog.With("email", email, "code", data.Code)

	if err := pm.mailer.SendEmail(ctx, msg); err != nil {
		clog.ErrorContext(ctx, "Failed to send two factor code", common.ErrAttr(err))

		return err
	}

	clog.InfoContext(ctx, "Sent two factor code")

	return nil
}

func (pm *PortalMailer) SendWelcome(ctx context.Context, email string) error {
	data := struct {
		Domain      string
		CurrentYear int
		CDN         string
	}{
		CDN:         pm.cdn,
		Domain:      pm.domain,
		CurrentYear: time.Now().Year(),
	}

	var htmlBodyTpl bytes.Buffer
	if err := pm.welcomeHTMLTemplate.Execute(&htmlBodyTpl, data); err != nil {
		return err
	}

	var textBodyTpl bytes.Buffer
	if err := pm.welcomeTextTemplate.Execute(&textBodyTpl, data); err != nil {
		return err
	}

	msg := &Message{
		HTMLBody:  htmlBodyTpl.String(),
		TextBody:  textBodyTpl.String(),
		Subject:   "Welcome to Private Captcha",
		EmailTo:   email,
		EmailFrom: pm.emailFrom.Value(),
		NameFrom:  common.PrivateCaptcha,
		ReplyTo:   email,
	}

	if err := pm.mailer.SendEmail(ctx, msg); err != nil {
		slog.ErrorContext(ctx, "Failed to send welcome email", common.ErrAttr(err))

		return err
	}

	slog.InfoContext(ctx, "Sent welcome email", "email", email)

	return nil
}
