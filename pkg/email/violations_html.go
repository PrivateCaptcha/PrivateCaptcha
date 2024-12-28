package email

const (
	violationsTextTemplate = `
Accounts with usage violations in {{.Stage}}:
{{ range $email := .Emails }}
  - {{ $email }}
{{ end }}
`
)
