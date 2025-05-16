package portal

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type Jobs interface {
	OnboardUserJob(user *dbgen.User) common.OneOffJob
}
