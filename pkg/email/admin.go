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

type AdminMailer struct {
	stage                  string
	mailer                 *simpleMailer
	emailFrom              string
	emailTo                string
	violationsTextTemplate *template.Template
}

var _ common.AdminMailer = (*AdminMailer)(nil)

func NewAdminMailer(mailer *simpleMailer, getenv func(string) string) *AdminMailer {
	return &AdminMailer{
		stage:                  getenv("STAGE"),
		mailer:                 mailer,
		emailFrom:              getenv("PC_EMAIL_FROM"),
		emailTo:                getenv("PC_ADMIN_EMAIL"),
		violationsTextTemplate: template.Must(template.New("TextBody").Parse(violationsTextTemplate)),
	}
}

func (am *AdminMailer) SendUsageViolations(ctx context.Context, emails []string) error {
	data := struct {
		Emails      []string
		CurrentYear int
		Stage       string
	}{
		Emails:      emails,
		CurrentYear: time.Now().Year(),
		Stage:       am.stage,
	}

	var textBodyTpl bytes.Buffer
	if err := am.violationsTextTemplate.Execute(&textBodyTpl, data); err != nil {
		return err
	}

	msg := &Message{
		TextBody:  textBodyTpl.String(),
		Subject:   fmt.Sprintf("[%s] Usage violations found", am.stage),
		EmailTo:   am.emailTo,
		EmailFrom: am.emailFrom,
		NameFrom:  common.PrivateCaptcha,
	}
	err := am.mailer.SendEmail(ctx, msg)
	slog.InfoContext(ctx, "Sent usage violations email", "count", len(emails), common.ErrAttr(err))

	return err
}
