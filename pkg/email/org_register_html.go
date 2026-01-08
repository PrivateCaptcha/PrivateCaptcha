package email

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

type OrgRegisterInvitationContext struct {
	OrgName       string
	OrgOwnerName  string
	OrgOwnerEmail string
	RegisterURL   string
}

var (
	OrgRegisterInvitationTemplate = common.NewEmailTemplate("org-register-invitation", orgRegisterInvitationHTMLTemplate, orgRegisterInvitationTextTemplate)
)

const (
	orgRegisterInvitationHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
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
            <p style="font-size:16px;line-height:26px;margin:32px 0 16px">
            Hello,
            </p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">
            <strong>{{.OrgOwnerName}}</strong> (<a href="mailto:{{.OrgOwnerEmail}}">{{.OrgOwnerEmail}}</a>) has invited you to the <strong>{{.OrgName}}</strong> organization in Private Captcha.
            </p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">
            To accept this invitation, you'll need to create an account first.
            </p>
            <table
              border="0"
              cellpadding="0"
              cellspacing="0"
              role="presentation"
              style="margin-top:32px;margin-bottom:32px;">
              <tbody>
                <tr>
                  <td>
                    <a
                      href="{{.RegisterURL}}"
                      style="border-radius:0.5rem;background-color:rgb(0,0,0);padding-left:20px;padding-right:20px;padding-top:12px;padding-bottom:12px;text-align:center;font-weight:600;font-size:16px;color:rgb(255,255,255);text-decoration-line:none;line-height:100%;text-decoration:none;display:inline-block;max-width:100%;mso-padding-alt:0px"
                      target="_blank"
                      ><span
                        ><!--[if mso]><i style="mso-font-width:500%;mso-text-raise:18" hidden>&#8202;&#8202;</i><![endif]--></span
                      ><span
                        style="max-width:100%;display:inline-block;line-height:120%;mso-padding-alt:0px;mso-text-raise:9px"
                        >Create account &amp; join</span
                      ><span
                        ><!--[if mso]><i style="mso-font-width:500%" hidden>&#8202;&#8202;&#8203;</i><![endif]--></span
                      ></a
                    >
                  </td>
                </tr>
              </tbody>
            </table>
            <p style="font-size:16px;line-height:26px;margin:16px 0">
              Warmly,<br />The Private Captcha team
            </p>
            <hr style="width:100%;border:none;border-top:1px solid #eaeaea;border-color:#cccccc;margin:20px 0" />
            <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
                <a href="https://privatecaptcha.com" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> © {{.CurrentYear}} Intmaker OÜ
            </p>
          </td>
        </tr>
      </tbody>
    </table>
  </body>
</html>`
	orgRegisterInvitationTextTemplate = `Hello,

{{.OrgOwnerName}} ({{.OrgOwnerEmail}}) has invited you to the '{{.OrgName}}' organization in Private Captcha.

To accept this invitation, you'll need to create an account first.

Create account & join by following this link: {{.RegisterURL}}

Warmly,
The Private Captcha team

--

PrivateCaptcha © {{.CurrentYear}} Intmaker OÜ`
)
