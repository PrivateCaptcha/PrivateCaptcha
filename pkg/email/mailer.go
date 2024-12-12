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
	HTMLBody string
	TextBody string
	Subject  string
	Email    string
	Name     string
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

type SimpleMailer struct {
	URL      string
	Username string
	Password string
	Sender   string
}

func (sm *SimpleMailer) SendEmail(ctx context.Context, msg *Message) error {
	if len(msg.Email) == 0 {
		return errors.New("email is empty")
	}

	if len(sm.Sender) == 0 {
		return errors.New("sender is empty")
	}

	dialer, err := smtpDialer(sm.URL, sm.Username, sm.Password)
	if err != nil {
		return err
	}

	m := gomail.NewMessage()

	m.SetAddressHeader("To", msg.Email, msg.Name)
	m.SetAddressHeader("From", sm.Sender, "Private Captcha")
	m.SetHeader("Subject", msg.Subject)
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
		return errors.New("no email body was generated")
	}

	err = dialer.DialAndSend(m)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to send an email", "host", dialer.Host, "port", dialer.Port, common.ErrAttr(err))
		return err
	}

	return nil
}
