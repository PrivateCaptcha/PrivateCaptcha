package email

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/go-gomail/gomail"
)

type Message struct {
	HTMLBody  string
	TextBody  string
	Subject   string
	EmailTo   string
	NameTo    string
	EmailFrom string
	NameFrom  string
	ReplyTo   string
}

var (
	errInvalidMessage = errors.New("mail message is not valid")
	errNoEmailBody    = errors.New("no email body was generated")
)

func (m *Message) Valid() bool {
	return (m != nil) &&
		len(m.EmailTo) > 0 &&
		len(m.EmailFrom) > 0 &&
		(len(m.HTMLBody) > 0 || len(m.TextBody) > 0)
}

func smtpDialer(smtpURL, user, pass string) (*gomail.Dialer, error) {
	surl, err := url.Parse(smtpURL)
	if err != nil {
		return nil, err
	}

	// Port
	var port int
	if i, err := strconv.Atoi(surl.Port()); err == nil {
		port = i
	} else if surl.Scheme == "smtp" {
		port = 25
	} else {
		port = 465
	}

	d := gomail.NewDialer(surl.Hostname(), port, user, pass)
	if surl.Scheme == "smtps" {
		d.SSL = true
	}

	return d, nil
}

func NewMailSender(cfg common.ConfigStore) *simpleMailer {
	return NewMailSenderEx(cfg.Get(common.SmtpEndpointKey), cfg.Get(common.SmtpUsernameKey), cfg.Get(common.SmtpPasswordKey))
}

func NewMailSenderEx(endpoint, username, password common.ConfigItem) *simpleMailer {
	return &simpleMailer{
		endpoint: endpoint,
		username: username,
		password: password,
	}
}

type Sender interface {
	SendEmail(ctx context.Context, msg *Message) error
}

type simpleMailer struct {
	endpoint common.ConfigItem
	username common.ConfigItem
	password common.ConfigItem
}

var _ Sender = (*simpleMailer)(nil)

func (sm *simpleMailer) SendEmail(ctx context.Context, msg *Message) error {
	if !msg.Valid() {
		return errInvalidMessage
	}

	m := gomail.NewMessage()

	m.SetAddressHeader("To", msg.EmailTo, msg.NameTo)
	m.SetAddressHeader("From", msg.EmailFrom, msg.NameFrom)
	m.SetHeader("Subject", msg.Subject)
	if len(msg.ReplyTo) > 0 {
		m.SetHeader("Reply-To", msg.ReplyTo)

	}
	//m.SetHeader("X-Mailer", xMailer)

	hasBody := false
	if len(msg.TextBody) > 0 {
		m.SetBody("text/plain", msg.TextBody)
		hasBody = true
	}
	if len(msg.HTMLBody) > 0 {
		m.AddAlternative("text/html", msg.HTMLBody)
		hasBody = true
	}
	if !hasBody {
		return errNoEmailBody
	}

	dialer, err := smtpDialer(sm.endpoint.Value(), sm.username.Value(), sm.password.Value())
	if err != nil {
		return err
	}

	err = dialer.DialAndSend(m)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to send an email", "email", msg.EmailTo, "host", dialer.Host, "port", dialer.Port,
			common.ErrAttr(err))
		return err
	}

	return nil
}
