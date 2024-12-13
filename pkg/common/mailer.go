package common

import (
	"context"
	"time"
)

type AdminMailer interface {
	SendUsageViolations(ctx context.Context, emails []string) error
}

type SupportRequest struct {
	Category string
	Text     string
	TicketID string
}

func (r *SupportRequest) ShortTicketID() string {
	if len(r.TicketID) > 13 {
		return r.TicketID[:13]
	}

	return time.Now().Format(time.DateOnly)
}

type Mailer interface {
	SendTwoFactor(ctx context.Context, email string, code int) error
	SendSupportRequest(ctx context.Context, email string, req *SupportRequest) error
}
