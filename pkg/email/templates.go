package email

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	templates = []*common.EmailTemplate{
		APIKeyExpirationTemplate,
		APIKeyExpiredTemplate,
		WelcomeEmailTemplate,
		TwoFactorEmailTemplate,
		OrgInvitationTemplate,
		WeeklyReportTemplate,
		MonthlyReportTemplate,
	}
)

func Templates() []*common.EmailTemplate {
	return templates
}
