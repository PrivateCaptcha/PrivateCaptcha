package common

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	htmltpl "html/template"
	"log/slog"
	"sync"
	texttpl "text/template"
	"time"
)

type Mailer interface {
	SendTwoFactor(ctx context.Context, email string, code int, ua string, location string, isRegistration bool) error
	SendWelcome(ctx context.Context, email, name string) error
	SendOrgInvite(ctx context.Context, email, name string, orgName, orgOwnerEmail, orgOwnerName, orgURL string, requiresRegister bool) error
}

type NotificationCondition int

const (
	EmptyNotificationCondition NotificationCondition = iota
	NotificationWithSubscription
	NotificationWithoutSubscription
)

type ScheduledNotification struct {
	ReferenceID  string
	UserID       int32
	Subject      string
	Data         interface{}
	DateTime     time.Time
	TemplateHash string
	PersistUntil *time.Time
	Condition    NotificationCondition
	EmailFrom    *string
	ReplyToEmail *string
	EmailTo      *string
}

func NewEmailTemplate(name, contentHTML, contentText string, funcs htmltpl.FuncMap) *EmailTemplate {
	return &EmailTemplate{
		name:        name,
		contentHTML: contentHTML,
		contentText: contentText,
		funcs:       funcs,
	}
}

type EmailTemplate struct {
	name        string
	hash        string
	mux         sync.Mutex
	contentHTML string
	contentText string

	// Parsed templates - lazy initialized
	funcs      htmltpl.FuncMap
	parsedHTML *htmltpl.Template
	parsedText *texttpl.Template
	parseOnce  sync.Once
}

func (et *EmailTemplate) Name() string        { return et.name }
func (et *EmailTemplate) ContentHTML() string { return et.contentHTML }
func (et *EmailTemplate) ContentText() string { return et.contentText }

func (et *EmailTemplate) Hash() string {
	et.mux.Lock()
	defer et.mux.Unlock()

	if len(et.hash) == 0 {
		h := sha1.New()
		if len(et.contentHTML) > 0 {
			h.Write([]byte(et.contentHTML))
		} else if len(et.contentText) > 0 {
			h.Write([]byte(et.contentText))
		} else {
			h.Write([]byte(et.name))
		}
		et.hash = hex.EncodeToString(h.Sum(nil))
	}

	return et.hash
}

func (et *EmailTemplate) ensureParsed(ctx context.Context) {
	et.parseOnce.Do(func() {
		if len(et.contentHTML) > 0 {
			if tpl, err := htmltpl.New("HtmlBody").Funcs(et.funcs).Parse(et.contentHTML); err != nil {
				slog.ErrorContext(ctx, "Failed to parse HTML template", ErrAttr(err))
			} else {
				et.parsedHTML = tpl
			}
		}
		if len(et.contentText) > 0 {
			if tpl, err := texttpl.New("TextBody").Funcs(et.funcs).Parse(et.contentText); err != nil {
				slog.ErrorContext(ctx, "Failed to parse text template", ErrAttr(err))
			} else {
				et.parsedText = tpl
			}
		}
	})
}

func (et *EmailTemplate) RenderHTML(ctx context.Context, data interface{}) (string, error) {
	et.ensureParsed(ctx)

	var buf bytes.Buffer
	if et.parsedHTML != nil {
		if err := et.parsedHTML.Execute(&buf, data); err != nil {
			slog.ErrorContext(ctx, "Failed to execute HTML template", ErrAttr(err))
			return "", err
		}
	}

	return buf.String(), nil
}

func (et *EmailTemplate) RenderText(ctx context.Context, data interface{}) (string, error) {
	et.ensureParsed(ctx)

	var buf bytes.Buffer
	if et.parsedText != nil {
		if err := et.parsedText.Execute(&buf, data); err != nil {
			slog.ErrorContext(ctx, "Failed to execute Text template", ErrAttr(err))
			return "", err
		}
	}

	return buf.String(), nil
}
