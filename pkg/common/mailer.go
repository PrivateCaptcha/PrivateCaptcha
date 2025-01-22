package common

import (
	"context"
	"fmt"
	"time"
)

type AdminMailer interface {
	SendUsageViolations(ctx context.Context, emails []string) error
}

type SupportRequest struct {
	Category string
	Subject  string
	Text     string
	TicketID string
}

func (r *SupportRequest) ShortTicketID() string {
	if len(r.TicketID) > 13 {
		return r.TicketID[:13]
	}

	return time.Now().Format(time.DateOnly)
}

func (r *SupportRequest) EmailSubject() string {
	if len(r.Subject) > 0 {
		const maxSubjectLength = 100
		length := min(len(r.Subject), maxSubjectLength)
		subject := r.Subject[:length]
		if len(r.Subject) > maxSubjectLength {
			subject += "..."
		}
		return fmt.Sprintf("[%s] %s", PrivateCaptcha, subject)
	}

	return fmt.Sprintf("[%s] Support request %s", PrivateCaptcha, r.ShortTicketID())
}

type Mailer interface {
	SendTwoFactor(ctx context.Context, email string, code int) error
	SendSupportRequest(ctx context.Context, email string, req *SupportRequest) error
	SendSupportAck(ctx context.Context, email string, req *SupportRequest) error
	SendWelcome(ctx context.Context, email string) error
}
