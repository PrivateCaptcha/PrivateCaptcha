package portal

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type Jobs interface {
	OnboardUser(user *dbgen.User) common.OneOffJob
}
