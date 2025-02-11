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
	mailer                         *simpleMailer
	cdn                            string
	domain                         string
	emailFrom                      common.ConfigItem
	supportEmail                   common.ConfigItem
	adminEmail                     common.ConfigItem
	twofactorHTMLTemplate          *template.Template
	twofactorTextTemplate          *template.Template
	supportHTMLTemplate            *template.Template
	supportTextTemplate            *template.Template
	welcomeHTMLTemplate            *template.Template
	welcomeTextTemplate            *template.Template
	supportAcknowledgeHTMLTemplate *template.Template
	supportAcknowledgeTextTemplate *template.Template
}

func NewPortalMailer(cdn, domain string, mailer *simpleMailer, cfg common.ConfigStore) *PortalMailer {
	return &PortalMailer{
		mailer:                         mailer,
		emailFrom:                      cfg.Get(common.EmailFromKey),
		supportEmail:                   cfg.Get(common.SupportEmailKey),
		adminEmail:                     cfg.Get(common.AdminEmailKey),
		cdn:                            cdn,
		domain:                         domain,
		twofactorHTMLTemplate:          template.Must(template.New("HtmlBody").Parse(TwoFactorHTMLTemplate)),
		twofactorTextTemplate:          template.Must(template.New("TextBody").Parse(twoFactorTextTemplate)),
		supportHTMLTemplate:            template.Must(template.New("HtmlBody").Parse(SupportHTMLTemplate)),
		supportTextTemplate:            template.Must(template.New("TextBody").Parse(supportTextTemplate)),
		welcomeHTMLTemplate:            template.Must(template.New("HtmlBody").Parse(WelcomeHTMLTemplate)),
		welcomeTextTemplate:            template.Must(template.New("TextBody").Parse(welcomeTextTemplate)),
		supportAcknowledgeHTMLTemplate: template.Must(template.New("HtmlBody").Parse(SupportAcknowledgeHTMLTemplate)),
		supportAcknowledgeTextTemplate: template.Must(template.New("TextBody").Parse(supportAcknowledgeTextTemplate)),
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
		Domain:      pm.domain,
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
		level := slog.LevelError

		if email == pm.adminEmail.Value() {
			level = slog.LevelWarn
			err = nil
		}

		clog.Log(ctx, level, "Failed to send two factor code", common.ErrAttr(err))

		return err
	}

	clog.InfoContext(ctx, "Sent two factor code")

	return nil
}

func (pm *PortalMailer) SendSupportRequest(ctx context.Context, email string, req *common.SupportRequest) error {
	data := struct {
		Message     string
		Domain      string
		CurrentYear int
		TicketID    string
		CDN         string
	}{
		Message:     req.Text,
		CDN:         pm.cdn,
		Domain:      pm.domain,
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

	tlog := slog.With("ticketID", req.TicketID)

	msg := &Message{
		HTMLBody:  htmlBodyTpl.String(),
		TextBody:  textBodyTpl.String(),
		Subject:   req.EmailSubject(),
		EmailTo:   pm.supportEmail.Value(),
		EmailFrom: pm.emailFrom.Value(),
		NameFrom:  common.PrivateCaptcha,
		ReplyTo:   email,
	}

	if err := pm.mailer.SendEmail(ctx, msg); err != nil {
		tlog.ErrorContext(ctx, "Failed to send support request", common.ErrAttr(err))

		return err
	}

	tlog.InfoContext(ctx, "Sent support email", "email", email)

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

func (pm *PortalMailer) SendSupportAck(ctx context.Context, email string, req *common.SupportRequest) error {
	data := struct {
		Domain      string
		CurrentYear int
		TicketID    string
		CDN         string
	}{
		CDN:         pm.cdn,
		Domain:      pm.domain,
		CurrentYear: time.Now().Year(),
		TicketID:    req.TicketID,
	}

	var htmlBodyTpl bytes.Buffer
	if err := pm.supportAcknowledgeHTMLTemplate.Execute(&htmlBodyTpl, data); err != nil {
		return err
	}

	var textBodyTpl bytes.Buffer
	if err := pm.supportAcknowledgeHTMLTemplate.Execute(&textBodyTpl, data); err != nil {
		return err
	}

	tlog := slog.With("ticketID", req.TicketID)

	msg := &Message{
		HTMLBody:  htmlBodyTpl.String(),
		TextBody:  textBodyTpl.String(),
		Subject:   req.EmailSubject(),
		EmailTo:   email,
		EmailFrom: pm.emailFrom.Value(),
		NameFrom:  common.PrivateCaptcha,
		ReplyTo:   email,
	}

	if err := pm.mailer.SendEmail(ctx, msg); err != nil {
		tlog.ErrorContext(ctx, "Failed to send support acknowledgement", common.ErrAttr(err))

		return err
	}

	tlog.InfoContext(ctx, "Sent support acknowledgement", "email", email)

	return nil
}
