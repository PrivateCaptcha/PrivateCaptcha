package email

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

const (
	ColorGreen   = "#22883e"
	ColorRed     = "#c53030"
	ColorNeutral = "#888888"
)

type PropertyStat struct {
	Name        string
	Domain      string
	Count       uint64
	Percent     float64
	PrevCount   uint64
	Change      float64
	ChangeSign  string
	ChangeColor string
}

type UsageReportContext struct {
	Period           string
	TotalRequests    uint64
	TotalVerifies    uint64
	PrevRequests     uint64
	PrevVerifies     uint64
	RequestsChange   float64
	VerifiesChange   float64
	RequestsSign     string
	VerifiesSign     string
	RequestsColor    string
	VerifiesColor    string
	DashboardPath    string
	VerificationRate float64
	TopProperties    []PropertyStat
}

var (
	WeeklyReportTemplate  = common.NewEmailTemplate("weekly-report", weeklyReportHTMLTemplate, weeklyReportTextTemplate)
	MonthlyReportTemplate = common.NewEmailTemplate("monthly-report", monthlyReportHTMLTemplate, monthlyReportTextTemplate)
)

const (
	weeklyReportHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
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
              Here is your <strong>{{.Period}}</strong> Private Captcha usage report:
            </p>
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0;border-collapse:collapse">
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#666;text-align:center">Total Requests</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;font-weight:bold;text-align:center">{{.TotalRequests}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:13px;color:{{.RequestsColor}};text-align:center">{{.RequestsSign}}{{printf "%.1f" .RequestsChange}}%</td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#666;text-align:center;background-color:#f9f9f9">Total Verifications</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;font-weight:bold;text-align:center;background-color:#f9f9f9">{{.TotalVerifies}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:13px;color:{{.VerifiesColor}};text-align:center;background-color:#f9f9f9">{{.VerifiesSign}}{{printf "%.1f" .VerifiesChange}}%</td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#666;text-align:center">Verification Rate</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;font-weight:bold;text-align:center" colspan="2">{{printf "%.1f" .VerificationRate}}%</td>
              </tr>
            </table>
            {{- if .TopProperties}}
            <p style="font-size:16px;line-height:26px;margin:16px 0"><strong>Top properties by requests:</strong></p>
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0;border-collapse:collapse;width:100%">
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">Property</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">Domain</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">Requests</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">%</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">Change</td>
              </tr>
              {{- range .TopProperties}}
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:center">{{.Name}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#666;text-align:center">{{.Domain}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:center">{{.Count}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:center">{{printf "%.1f" .Percent}}%</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:center;color:{{.ChangeColor}}">{{.ChangeSign}}{{printf "%.1f" .Change}}%</td>
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
              You can manage your report preferences in <a href="{{.PortalURL}}/settings?tab=notifications" style="text-decoration:underline;color:#9ca299;">notification settings</a>.
            </p>
            <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
                <a href="https://privatecaptcha.com" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> &copy; {{.CurrentYear}} Intmaker OU
            </p>
          </td>
        </tr>
      </tbody>
    </table>
  </body>
</html>`
	weeklyReportTextTemplate = `Hello,

Here is your {{.Period}} Private Captcha usage report:

Total Requests: {{.TotalRequests}} ({{.RequestsSign}}{{printf "%.1f" .RequestsChange}}%)
Total Verifications: {{.TotalVerifies}} ({{.VerifiesSign}}{{printf "%.1f" .VerifiesChange}}%)
Verification Rate: {{printf "%.1f" .VerificationRate}}%
{{- if .TopProperties}}

Top properties by requests:
{{- range .TopProperties}}
  - {{.Name}} ({{.Domain}}): {{.Count}} requests ({{printf "%.1f" .Percent}}%, {{.ChangeSign}}{{printf "%.1f" .Change}}%)
{{- end}}
{{- end}}

View detailed statistics in your dashboard ({{.PortalURL}}/{{.DashboardPath}}).

Warmly,
The Private Captcha team

--

You can manage your report preferences in notification settings ({{.PortalURL}}/settings?tab=notifications).

PrivateCaptcha (c) {{.CurrentYear}} Intmaker OU`

	monthlyReportHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
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
              Here is your <strong>{{.Period}}</strong> Private Captcha usage report:
            </p>
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0;border-collapse:collapse">
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#666;text-align:center">Total Requests</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;font-weight:bold;text-align:center">{{.TotalRequests}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:13px;color:{{.RequestsColor}};text-align:center">{{.RequestsSign}}{{printf "%.1f" .RequestsChange}}%</td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#666;text-align:center;background-color:#f9f9f9">Total Verifications</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;font-weight:bold;text-align:center;background-color:#f9f9f9">{{.TotalVerifies}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:13px;color:{{.VerifiesColor}};text-align:center;background-color:#f9f9f9">{{.VerifiesSign}}{{printf "%.1f" .VerifiesChange}}%</td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#666;text-align:center">Verification Rate</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;font-weight:bold;text-align:center" colspan="2">{{printf "%.1f" .VerificationRate}}%</td>
              </tr>
            </table>
            {{- if .TopProperties}}
            <p style="font-size:16px;line-height:26px;margin:16px 0"><strong>Top properties by requests:</strong></p>
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0;border-collapse:collapse;width:100%">
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">Property</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">Domain</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">Requests</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">%</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:12px;color:#666;font-weight:bold;text-align:center">Change</td>
              </tr>
              {{- range .TopProperties}}
              <tr>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:center">{{.Name}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;color:#666;text-align:center">{{.Domain}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:center">{{.Count}}</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:center">{{printf "%.1f" .Percent}}%</td>
                <td style="padding:10px 20px;border:1px solid #dddddd;font-size:14px;text-align:center;color:{{.ChangeColor}}">{{.ChangeSign}}{{printf "%.1f" .Change}}%</td>
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
              You can manage your report preferences in <a href="{{.PortalURL}}/settings?tab=notifications" style="text-decoration:underline;color:#9ca299;">notification settings</a>.
            </p>
            <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
                <a href="https://privatecaptcha.com" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> &copy; {{.CurrentYear}} Intmaker OU
            </p>
          </td>
        </tr>
      </tbody>
    </table>
  </body>
</html>`
	monthlyReportTextTemplate = `Hello,

Here is your {{.Period}} Private Captcha usage report:

Total Requests: {{.TotalRequests}} ({{.RequestsSign}}{{printf "%.1f" .RequestsChange}}%)
Total Verifications: {{.TotalVerifies}} ({{.VerifiesSign}}{{printf "%.1f" .VerifiesChange}}%)
Verification Rate: {{printf "%.1f" .VerificationRate}}%
{{- if .TopProperties}}

Top properties by requests:
{{- range .TopProperties}}
  - {{.Name}} ({{.Domain}}): {{.Count}} requests ({{printf "%.1f" .Percent}}%, {{.ChangeSign}}{{printf "%.1f" .Change}}%)
{{- end}}
{{- end}}

View detailed statistics in your dashboard ({{.PortalURL}}/{{.DashboardPath}}).

Warmly,
The Private Captcha team

--

You can manage your report preferences in notification settings ({{.PortalURL}}/settings?tab=notifications).

PrivateCaptcha (c) {{.CurrentYear}} Intmaker OU`
)
