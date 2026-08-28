package db

import (
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type UserJobs interface {
	OnboardUser(user *dbgen.User, plan billing.Plan, orgInviteID *int32) common.OneOffJob
	OffboardUser(user *dbgen.User) common.OneOffJob
	LoginUser(sess *session.Session) common.OneOffJob
	CheckRegistration(sess *session.Session, r *http.Request) common.OneOffJob
	FinalizeRegistration(sess *session.Session, userID int32) common.OneOffJob
}
