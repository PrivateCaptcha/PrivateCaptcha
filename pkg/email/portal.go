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
	Domain                string
	emailFrom             string
	supportEmail          string
	twofactorHTMLTemplate *template.Template
	twofactorTextTemplate *template.Template
	supportHTMLTemplate   *template.Template
	supportTextTemplate   *template.Template
}

func NewPortalMailer(domain string, mailer *simpleMailer, getenv func(string) string) *PortalMailer {
	return &PortalMailer{
		mailer:                mailer,
		emailFrom:             getenv("PC_EMAIL_FROM"),
		supportEmail:          getenv("PC_SUPPORT_EMAIL"),
		Domain:                domain,
		twofactorHTMLTemplate: template.Must(template.New("HtmlBody").Parse(TwoFactorHTMLTemplate)),
		twofactorTextTemplate: template.Must(template.New("TextBody").Parse(twoFactorTextTemplate)),
		supportHTMLTemplate:   template.Must(template.New("HtmlBody").Parse(SupportHTMLTemplate)),
		supportTextTemplate:   template.Must(template.New("TextBody").Parse(supportTextTemplate)),
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
		HTMLBody:  htmlBodyTpl.String(),
		TextBody:  textBodyTpl.String(),
		Subject:   fmt.Sprintf("[%s] Your verification code", common.PrivateCaptcha),
		EmailTo:   email,
		EmailFrom: pm.emailFrom,
		NameFrom:  common.PrivateCaptcha,
	}
	err := pm.mailer.SendEmail(ctx, msg)
	slog.InfoContext(ctx, "Sent two factor code", "email", email, "code", code, common.ErrAttr(err))

	return err
}

func (pm *PortalMailer) SendSupportRequest(ctx context.Context, email string, req *common.SupportRequest) error {
	data := struct {
		Message     string
		Domain      string
		CurrentYear int
		TicketID    string
	}{
		Message:     req.Text,
		Domain:      pm.Domain,
		CurrentYear: time.Now().Year(),
		TicketID:    req.TicketID,
	}

	var htmlBodyTpl bytes.Buffer
	if err := pm.supportHTMLTemplate.Execute(&htmlBodyTpl, data); err != nil {
		return err
	}

	var textBodyTpl bytes.Buffer
	if err := pm.supportTextTemplate.Execute(&textBodyTpl, data); err != nil {
		return err
	}

	msg := &Message{
		HTMLBody:  htmlBodyTpl.String(),
		TextBody:  textBodyTpl.String(),
		Subject:   fmt.Sprintf("[%s] Support request %s", req.Category, req.ShortTicketID()),
		EmailTo:   pm.supportEmail,
		EmailFrom: pm.emailFrom,
		NameFrom:  common.PrivateCaptcha,
		ReplyTo:   email,
	}
	err := pm.mailer.SendEmail(ctx, msg)
	slog.InfoContext(ctx, "Sent support email", "email", email, common.ErrAttr(err))

	return err
}
