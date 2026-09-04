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
		FormDeactivationTemplate,
	}
	emailFuncs = template.FuncMap{
		"sub": func(a, b int) int {
			return a - b
		},
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
		"default": func(def, val any) any {
			if (val == nil) || (val == "") || (val == 0) || (val == false) {
				return def
			}
			return val
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
	r := []rune(s)

	if n <= 3 {
		if len(r) <= n {
			return s
		}
		return "..."
	}

	if len(r) <= n {
		return s
	}

	return string(r[:n-3]) + "..."
}
