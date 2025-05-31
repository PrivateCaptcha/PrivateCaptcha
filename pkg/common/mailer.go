package common

import (
	"context"
)

type Mailer interface {
	SendTwoFactor(ctx context.Context, email string, code int) error
	SendWelcome(ctx context.Context, email string) error
}
