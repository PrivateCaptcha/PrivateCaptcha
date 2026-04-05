package email

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

type PropertyStat struct {
	Name      string
	Domain    string
	Count     uint64
	Percent   float64
	Change    float64
	Alternate bool
}

type UsageReportContext struct {
	Period                 string
	PeriodDate             string
	TotalRequests          uint64
	TotalVerifies          uint64
	PrevRequests           uint64
	PrevVerifies           uint64
	RequestsChange         float64
	VerifiesChange         float64
	VerificationRateChange float64
	DashboardPath          string
	VerificationRate       float64
	AccountLimit           uint64
	TopProperties          []*PropertyStat
}

var (
	UsageReportTemplate = common.NewEmailTemplate("usage-report", usageReportHTMLTemplate, usageReportTextTemplate)
)

const (
	usageReportHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html dir="ltr" lang="en">
  <head>
    <link rel="preload" as="image" href="{{.CDNURL}}/portal/img/pc-logo-dark.png" />
    <meta content="text/html; charset=UTF-8" http-equiv="Content-Type" />
    <meta name="x-apple-disable-message-reformatting" />
  </head>
  <body
    style='background-color:#ffffff;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Oxygen-Sans,Ubuntu,Cantarell,"Helvetica Neue",sans-serif'
  >
    <table
      align="center"
      width="100%"
      border="0"
      cellpadding="0"
      cellspacing="0"
      role="presentation"
      style="max-width:37.5em;margin:0 auto;padding:20px 0 48px"
    >
      <tbody>
        <tr style="width:100%">
          <td>
            <img alt="Private Captcha" height="40" src="{{.CDNURL}}/portal/img/pc-logo-dark.png" style="display:block;outline:none;border:none;text-decoration:none" />
            <p style="font-size:16px;line-height:32px;margin:24px 0 16px">
              Hello,
            </p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">
              Here is your {{.Period}}{{if .PeriodDate}} ({{.PeriodDate}}){{end}} Private Captcha usage report:
            </p>
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0 24px;border-collapse:collapse">
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#000000;text-align:left">Total Requests</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.TotalRequests | humanize}}</span></td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:13px;text-align:right;{{if gt .RequestsChange 0.0}}color:#22883e{{else if lt .RequestsChange 0.0}}color:#c53030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .RequestsChange 0.0}}+{{end}}{{printf "%.1f" .RequestsChange}}%</span></td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#000000;text-align:left">Total Verifications</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.TotalVerifies | humanize}}</span></td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:13px;text-align:right;{{if gt .VerifiesChange 0.0}}color:#22883e{{else if lt .VerifiesChange 0.0}}color:#c53030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .VerifiesChange 0.0}}+{{end}}{{printf "%.1f" .VerifiesChange}}%</span></td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#000000;text-align:left">Verification Rate</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{printf "%.1f" .VerificationRate}}%</span></td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:13px;text-align:right;{{if gt .VerificationRateChange 0.0}}color:#22883e{{else if lt .VerificationRateChange 0.0}}color:#c53030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .VerificationRateChange 0.0}}+{{end}}{{printf "%.1f" .VerificationRateChange}}%</span></td>
              </tr>
              {{- if .AccountLimit}}{{if or (gt .TotalRequests .AccountLimit) (gt .TotalVerifies .AccountLimit)}}
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#c53030;text-align:left">Account Limit</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace;color:#c53030'>{{.AccountLimit | humanize}}</span></td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:13px;text-align:right"></td>
              </tr>
              {{- end}}{{end}}
            </table>
            {{- if .TopProperties}}
            <p style="font-size:16px;line-height:26px;margin:16px 0">Top {{len .TopProperties}} properties by requests:</p>
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0 24px;border-collapse:collapse;width:100%">
              <tr>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;color:#000000;font-weight:bold;text-align:left;">Property</td>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;color:#000000;font-weight:bold;text-align:left">Domain</td>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;color:#000000;font-weight:bold;text-align:right">Requests</td>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;color:#000000;font-weight:bold;text-align:right">%</td>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;color:#000000;font-weight:bold;text-align:right">Change</td>
              </tr>
              {{- range .TopProperties}}
              <tr{{if .Alternate}} style="background-color:#f9f9f9"{{end}}>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;text-align:left" title="{{.Name}}">{{truncate .Name 24}}</td>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;text-align:left" title="{{.Domain}}">{{truncate .Domain 24}}</td>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.Count | humanize}}</span></td>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{printf "%.1f" .Percent}}%</span></td>
                <td style="padding:12px 16px;border:1px solid #dddddd;font-size:14px;text-align:right;{{if gt .Change 0.0}}color:#22883e{{else if lt .Change 0.0}}color:#c53030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .Change 0.0}}+{{end}}{{printf "%.1f" .Change}}%</span></td>
              </tr>
              {{- end}}
            </table>
            {{- end}}
            <p style="font-size:16px;line-height:26px;margin:16px 0">View detailed statistics in your <a href="{{.PortalURL}}/{{.DashboardPath}}">dashboard</a>.</p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">
              Warmly,<br />The Private Captcha team
            </p>
            <hr style="width:100%;border:none;border-top:1px solid #eaeaea;border-color:#cccccc;margin:20px 0" />
            <p style="font-size:12px;line-height:24px;margin:16px 0;color:#9ca299">
              You can manage your report preferences in the <a href="{{.PortalURL}}/settings?tab=notifications" style="text-decoration:underline;color:#9ca299;">portal</a>.
            </p>
            <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
              <a href="https://privatecaptcha.com" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> © {{.CurrentYear}} Intmaker OÜ
            </p>
          </td>
        </tr>
      </tbody>
    </table>
  </body>
</html>`
	usageReportTextTemplate = `Hello,

Here is your {{.Period}} Private Captcha usage report:

Total Requests: {{.TotalRequests}} ({{if gt .RequestsChange 0.0}}+{{end}}{{printf "%.1f" .RequestsChange}}%)
Total Verifications: {{.TotalVerifies}} ({{if gt .VerifiesChange 0.0}}+{{end}}{{printf "%.1f" .VerifiesChange}}%)
Verification Rate: {{printf "%.1f" .VerificationRate}}% ({{if gt .VerificationRateChange 0.0}}+{{end}}{{printf "%.1f" .VerificationRateChange}}%)
{{- if .AccountLimit}}{{if or (gt .TotalRequests .AccountLimit) (gt .TotalVerifies .AccountLimit)}}
Account Limit: {{.AccountLimit}}
{{- end}}{{end}}
{{- if .TopProperties}}

Top {{len .TopProperties}} properties by requests:
{{- range .TopProperties}}
  - {{.Name}} ({{.Domain}}): {{.Count}} requests ({{printf "%.1f" .Percent}}%, {{if gt .Change 0.0}}+{{end}}{{printf "%.1f" .Change}}%)
{{- end}}
{{- end}}

View detailed statistics in your dashboard ({{.PortalURL}}/{{.DashboardPath}}).

Warmly,
The Private Captcha team

--

You can manage your report preferences in notification settings ({{.PortalURL}}/settings?tab=notifications).

PrivateCaptcha (c) {{.CurrentYear}} Intmaker OÜ`
)
