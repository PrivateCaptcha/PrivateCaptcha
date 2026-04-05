package email

import (
	"fmt"
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
			return truncate(s, n)
		},
		"humanize": func(input any) string {
			var v float64

			switch t := input.(type) {
			case int:
				v = float64(t)
			case int8:
				v = float64(t)
			case uint8:
				v = float64(t)
			case int16:
				v = float64(t)
			case uint16:
				v = float64(t)
			case int32:
				v = float64(t)
			case uint32:
				v = float64(t)
			case int64:
				v = float64(t)
			case uint64:
				v = float64(t)
			case float32:
				v = float64(t)
			case float64:
				v = t
			default:
				// If it's not a number, return a string representation or empty
				return fmt.Sprintf("%v", input)
			}

			return common.FormatMagnitude(v)
		},
	}
)

func Templates() []*common.EmailTemplate {
	return templates
}

func Functions() template.FuncMap {
	return emailFuncs
}

func truncate(s string, n int) string {
	if n <= 3 {
		if len(s) <= n {
			return s
		}
		return "..."
	}

	if len(s) <= n {
		return s
	}

	return s[:n-3] + "..."
}
