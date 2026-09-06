package email

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

type PropertyStat struct {
	Name      string
	Domain    string
	Link      string
	Count     uint64
	Percent   float64
	Change    float64
	Alternate bool
}

type SecurityEventStat struct {
	Name           string
	Link           string
	Date           string
	Requests       uint64
	Verifies       uint64
	FailedVerifies uint64
	Alternate      bool
}

type FormStat struct {
	Name      string
	URL       string
	Link      string
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
	SecurityEvents         []*SecurityEventStat
	TotalFormSubmissions   uint64
	TotalFormErrors        uint64
	PrevFormSubmissions    uint64
	PrevFormErrors         uint64
	FormSubmissionsChange  float64
	FormErrorsChange       float64
	FormErrorRateChange    float64
	FormErrorRate          float64
	TopForms               []*FormStat
	UTM                    string
}

var (
	UsageReportTemplate = common.NewEmailTemplate("usage-report", usageReportHTMLTemplate, usageReportTextTemplate, emailFuncs)
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
    {{ $utm := .UTM | default "utm_medium=email&utm_source=report" }}
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
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0 24px;border-collapse:separate;border-spacing:0">
              <tr>
                <td style="padding:10px 20px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;color:#000000;text-align:left; font-weight:bold; background-color: #ecf1f7;border-radius:2px 0 0 0">Total Requests</td>
                <td style="padding:10px 20px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.TotalRequests | humanize}}</span></td>
                <td style="padding:10px 20px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right;border-radius:0 2px 0 0;{{if gt .RequestsChange 0.0}}color:#437540{{else if lt .RequestsChange 0.0}}color:#c33030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .RequestsChange 0.0}}+{{end}}{{printf "%.1f" .RequestsChange}}%</span></td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;color:#000000;text-align:left; font-weight:bold; background-color: #ecf1f7">Total Verifications</td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.TotalVerifies | humanize}}</span></td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right;{{if gt .VerifiesChange 0.0}}color:#437540{{else if lt .VerifiesChange 0.0}}color:#c33030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .VerifiesChange 0.0}}+{{end}}{{printf "%.1f" .VerifiesChange}}%</span></td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;color:#000000;text-align:left; font-weight:bold; background-color: #ecf1f7;{{if not (and .AccountLimit (or (gt .TotalRequests .AccountLimit) (gt .TotalVerifies .AccountLimit)))}}border-radius:0 0 0 2px{{end}}">Verification Rate</td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{printf "%.1f" .VerificationRate}}%</span></td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right;{{if gt .VerificationRateChange 0.0}}color:#437540{{else if lt .VerificationRateChange 0.0}}color:#c33030{{else}}color:#888888{{end}};{{if not (and .AccountLimit (or (gt .TotalRequests .AccountLimit) (gt .TotalVerifies .AccountLimit)))}}border-radius:0 0 2px 0{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .VerificationRateChange 0.0}}+{{end}}{{printf "%.1f" .VerificationRateChange}}%</span></td>
              </tr>
              {{- if .AccountLimit}}{{if or (gt .TotalRequests .AccountLimit) (gt .TotalVerifies .AccountLimit)}}
              <tr>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;color:#c33030;text-align:left; font-weight:bold; background-color: #ecf1f7;border-radius:0 0 0 2px">Account Limit</td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace;color:#c33030'>{{.AccountLimit | humanize}}</span></td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right;border-radius:0 0 2px 0"></td>
              </tr>
              {{- end}}{{end}}
             </table>
            {{- if or .TotalFormSubmissions .PrevFormSubmissions .TotalFormErrors .PrevFormErrors}}
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0 24px;border-collapse:separate;border-spacing:0">
              <tr>
                <td style="padding:10px 20px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;color:#000000;text-align:left; font-weight:bold; background-color: #ecf1f7;border-radius:2px 0 0 0">Total Submissions</td>
                <td style="padding:10px 20px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.TotalFormSubmissions | humanize}}</span></td>
                <td style="padding:10px 20px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right;border-radius:0 2px 0 0;{{if gt .FormSubmissionsChange 0.0}}color:#437540{{else if lt .FormSubmissionsChange 0.0}}color:#c33030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .FormSubmissionsChange 0.0}}+{{end}}{{printf "%.1f" .FormSubmissionsChange}}%</span></td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;color:#000000;text-align:left; font-weight:bold; background-color: #ecf1f7">Total Errors</td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.TotalFormErrors | humanize}}</span></td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right;{{if gt .FormErrorsChange 0.0}}color:#c33030{{else if lt .FormErrorsChange 0.0}}color:#437540{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .FormErrorsChange 0.0}}+{{end}}{{printf "%.1f" .FormErrorsChange}}%</span></td>
              </tr>
              <tr>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;color:#000000;text-align:left; font-weight:bold; background-color: #ecf1f7;border-radius:0 0 0 2px">Error Rate</td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{printf "%.1f" .FormErrorRate}}%</span></td>
                <td style="padding:10px 20px;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right;border-radius:0 0 2px 0;{{if gt .FormErrorRateChange 0.0}}color:#c33030{{else if lt .FormErrorRateChange 0.0}}color:#437540{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .FormErrorRateChange 0.0}}+{{end}}{{printf "%.1f" .FormErrorRateChange}}%</span></td>
              </tr>
            </table>
            {{- end}}
            {{- if .SecurityEvents}}
            <p style="font-size:16px;line-height:26px;margin:16px 0">Notable security events:</p>
            <table border="0" cellpadding="0" cellspacing="0" style="margin:16px 0 24px;border-collapse:separate;border-spacing:0;width:100%">
              <tr style="background-color: #ecf1f7; font-size:14px;color:#000000;font-weight:bold;">
                <th scope="col" style="padding:10px 12px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:left;border-radius:2px 0 0 0">Property</th>
                <th scope="col" style="padding:10px 12px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:left">Date</th>
                <th scope="col" style="padding:10px 12px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:right">Requests</th>
                <th scope="col" style="padding:10px 12px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:right;border-radius:0 2px 0 0">Verifications</th>
              </tr>
              {{- range $i, $event := .SecurityEvents}}
              <tr{{if .Alternate}} style="background-color:#f9faf5"{{end}}>
                <td style="padding:10px 12px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:left;{{if eq $i (sub (len $.SecurityEvents) 1)}}border-radius:0 0 0 2px{{end}}" title="{{.Name}}">
                  {{if .Link}}
                  <a href="{{.Link}}" style="color:#000000;text-decoration:none">
                    {{truncate .Name 24}} <span style="font-size:12px">&#8599;</span>
                  </a>
                  {{else}}
                  {{truncate .Name 24}}
                  {{end}}
                </td>
                <td style="padding:10px 12px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:left;white-space:nowrap">{{.Date}}</td>
                <td style="padding:10px 12px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.Requests | humanize}}</span></td>
                <td style="padding:10px 12px;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:13px;text-align:right;{{if eq $i (sub (len $.SecurityEvents) 1)}}border-radius:0 0 2px 0{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.Verifies | humanize}}{{if .FailedVerifies}} ({{.FailedVerifies | humanize}} failed){{end}}</span></td>
              </tr>
              {{- end}}
            </table>
            {{- end}}
            {{- if .TopProperties}}
            <p style="font-size:16px;line-height:26px;margin:16px 0">Top {{len .TopProperties}} properties by requests:</p>
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0 24px;border-collapse:separate;border-spacing:0;width:100%">
              <tr style="background-color: #ecf1f7; font-size:14px;color:#000000;font-weight:bold;">
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:left;border-radius:2px 0 0 0;">Property</td>
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:left">Domain</td>
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:right">Requests</td>
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:right">%</td>
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:right;border-radius:0 2px 0 0">Change</td>
              </tr>
              {{- range $i, $property := .TopProperties}}
              <tr{{if .Alternate}} style="background-color:#f9faf5"{{end}}>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:left;{{if eq $i (sub (len $.TopProperties) 1)}}border-radius:0 0 0 2px{{end}}" title="{{.Name}}">
                  {{if .Link}}
                  <a href="{{.Link}}" style="color:#000000;text-decoration:none">
                    {{truncate .Name 24}} <span style="font-size:12px">&#8599;</span>
                  </a>
                  {{else}}
                  {{truncate .Name 24}}
                  {{end}}
                </td>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:left" title="{{.Domain}}">{{truncate .Domain 24}}</td>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.Count | humanize}}</span></td>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{printf "%.1f" .Percent}}%</span></td>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right;{{if eq $i (sub (len $.TopProperties) 1)}}border-radius:0 0 2px 0;{{end}}{{if gt .Change 0.0}}color:#437540{{else if lt .Change 0.0}}color:#c33030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .Change 0.0}}+{{end}}{{printf "%.1f" .Change}}%</span></td>
              </tr>
              {{- end}}
            </table>
            {{- end}}
            {{- if .TopForms}}
            <p style="font-size:16px;line-height:26px;margin:16px 0">Top {{len .TopForms}} forms by submissions:</p>
            <table border="0" cellpadding="0" cellspacing="0" role="presentation" style="margin:16px 0 24px;border-collapse:separate;border-spacing:0;width:100%">
              <tr style="background-color: #ecf1f7; font-size:14px;color:#000000;font-weight:bold;">
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:left;border-radius:2px 0 0 0;">Form</td>
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:left">URL</td>
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:right">Submissions</td>
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:right">%</td>
                <td style="padding:12px 16px;border-top:1px dashed #bfbebc;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;text-align:right;border-radius:0 2px 0 0">Change</td>
              </tr>
              {{- range $i, $form := .TopForms}}
              <tr{{if .Alternate}} style="background-color:#f9faf5"{{end}}>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:left;{{if eq $i (sub (len $.TopForms) 1)}}border-radius:0 0 0 2px{{end}}" title="{{.Name}}">
                  {{if .Link}}
                  <a href="{{.Link}}" style="color:#000000;text-decoration:none">
                    {{truncate .Name 24}} <span style="font-size:12px">&#8599;</span>
                  </a>
                  {{else}}
                  {{truncate .Name 24}}
                  {{end}}
                </td>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:left" title="{{.URL}}">{{truncate .URL 24}}</td>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{.Count | humanize}}</span></td>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{printf "%.1f" .Percent}}%</span></td>
                <td style="padding:12px 16px;border-left:1px dashed #bfbebc;border-right:1px dashed #bfbebc;border-bottom:1px dashed #bfbebc;font-size:14px;text-align:right;{{if eq $i (sub (len $.TopForms) 1)}}border-radius:0 0 2px 0;{{end}}{{if gt .Change 0.0}}color:#437540{{else if lt .Change 0.0}}color:#c33030{{else}}color:#888888{{end}}"><span style='font-family:Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace'>{{if gt .Change 0.0}}+{{end}}{{printf "%.1f" .Change}}%</span></td>
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
              You can manage your report preferences in the <a href="{{.PortalURL}}/settings?tab=notifications&{{$utm}}" style="text-decoration:underline;color:#9ca299;">portal</a>.
            </p>
            <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
              <a href="https://privatecaptcha.com?{{$utm}}" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> © {{.CurrentYear}} Intmaker OÜ
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
{{- if or .TotalFormSubmissions .PrevFormSubmissions .TotalFormErrors .PrevFormErrors}}

Total Submissions: {{.TotalFormSubmissions}} ({{if gt .FormSubmissionsChange 0.0}}+{{end}}{{printf "%.1f" .FormSubmissionsChange}}%)
Total Errors: {{.TotalFormErrors}} ({{if gt .FormErrorsChange 0.0}}+{{end}}{{printf "%.1f" .FormErrorsChange}}%)
Error Rate: {{printf "%.1f" .FormErrorRate}}% ({{if gt .FormErrorRateChange 0.0}}+{{end}}{{printf "%.1f" .FormErrorRateChange}}%)
{{- end}}
{{- if .SecurityEvents}}

Notable security events:
{{- range .SecurityEvents}}
  - {{.Name}} | {{.Date}} UTC | {{.Requests}} requests | {{.Verifies}}{{if .FailedVerifies}} ({{.FailedVerifies}} failed){{end}} verifications{{if .Link}} | {{.Link}}{{end}}
{{- end}}
{{- end}}
{{- if .TopProperties}}

Top {{len .TopProperties}} properties by requests:
{{- range .TopProperties}}
  - {{.Name}} ({{.Domain}}): {{.Count}} requests ({{printf "%.1f" .Percent}}%, {{if gt .Change 0.0}}+{{end}}{{printf "%.1f" .Change}}%)
{{- end}}
{{- end}}
{{- if .TopForms}}

Top {{len .TopForms}} forms by submissions:
{{- range .TopForms}}
  - {{.Name}} ({{.URL}}): {{.Count}} submissions ({{printf "%.1f" .Percent}}%, {{if gt .Change 0.0}}+{{end}}{{printf "%.1f" .Change}}%)
{{- end}}
{{- end}}

View detailed statistics in your dashboard ({{.PortalURL}}/{{.DashboardPath}}).

Warmly,
The Private Captcha team

--

You can manage your report preferences in notification settings ({{.PortalURL}}/settings?tab=notifications).

PrivateCaptcha (c) {{.CurrentYear}} Intmaker OÜ`
)
