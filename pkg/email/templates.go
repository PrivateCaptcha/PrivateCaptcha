package email

import (
	"html/template"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	templates = []*common.EmailTemplate{
		APIKeyExpirationTemplate,
		APIKeyExpiredTemplate,
		WelcomeEmailTemplate,
		TwoFactorEmailTemplate,
		OrgInvitationTemplate,
		UsageReportTemplate,
	}
	emailFuncs = template.FuncMap{
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n-3] + "..."
		},
	}
)

func Templates() []*common.EmailTemplate {
	return templates
}

func Functions() template.FuncMap {
	return emailFuncs
}
