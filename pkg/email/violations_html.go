package email

const (
	violationsTextTemplate = `
Accounts with usage violations:
{{ range $email := .Emails }}
  - {{ $email }}
{{ end }}
`
)
