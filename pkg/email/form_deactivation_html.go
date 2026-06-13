package email

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

type DeactivatedForm struct {
	Name string
	Link string
}

type FormDeactivationContext struct {
	Forms []*DeactivatedForm
	UTM   string
}

var (
	FormDeactivationTemplate = common.NewEmailTemplate("form-deactivation", formDeactivationHTMLTemplate, formDeactivationTextTemplate, emailFuncs)
)

const (
	formDeactivationHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html dir="ltr" lang="en">
  <head>
    <link rel="preload" as="image" href="{{.CDNURL}}/portal/img/pc-logo-dark.png" />
    <meta content="text/html; charset=UTF-8" http-equiv="Content-Type" />
    <meta name="x-apple-disable-message-reformatting" />
  </head>
  <body style='background-color:#ffffff;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Oxygen-Sans,Ubuntu,Cantarell,"Helvetica Neue",sans-serif'>
    {{ $utm := .UTM | default "utm_medium=email&utm_source=form" }}
    <table align="center" width="100%" border="0" cellpadding="0" cellspacing="0" role="presentation" style="max-width:37.5em;margin:0 auto;padding:20px 0 48px">
      <tbody>
        <tr style="width:100%">
          <td>
            <img alt="Private Captcha" height="40" src="{{.CDNURL}}/portal/img/pc-logo-dark.png" style="display:block;outline:none;border:none;text-decoration:none" />
            <p style="font-size:16px;line-height:32px;margin:24px 0 16px">Hello,</p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">Some of your forms were deactivated due to multiple failed submissions. Please inspect them before reactivating:</p>
            <ul style="font-size:16px;line-height:26px;margin:16px 0;padding-left:24px">
              {{- range .Forms}}
              <li><a href="{{.Link}}" style="color:#000000;text-decoration:underline">{{.Name}}</a></li>
              {{- end}}
            </ul>
            <p style="font-size:16px;line-height:26px;margin:16px 0">Warmly,<br />The Private Captcha team</p>
            <hr style="width:100%;border:none;border-top:1px solid #eaeaea;border-color:#cccccc;margin:20px 0" />
            <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
              <a href="https://privatecaptcha.com?{{$utm}}" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> (c) {{.CurrentYear}} Intmaker OU
            </p>
          </td>
        </tr>
      </tbody>
    </table>
  </body>
</html>`

	formDeactivationTextTemplate = `Hello,

Some of your forms were deactivated due to multiple failed submissions. Please inspect them before reactivating:
{{- range .Forms}}
- {{.Name}} ({{.Link}})
{{- end}}

Warmly,
The Private Captcha team

--

PrivateCaptcha (c) {{.CurrentYear}} Intmaker OU`
)
