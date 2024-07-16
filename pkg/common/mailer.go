package common

import "context"

type AdminMailer interface {
	SendUsageViolations(ctx context.Context, emails []string) error
}

type Mailer interface {
	SendTwoFactor(ctx context.Context, email string, code int) error
	SendSupportRequest(ctx context.Context, email string, category string, message string) error
}
