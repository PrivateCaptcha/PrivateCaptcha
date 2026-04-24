package email

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

type APIKeyContext struct {
	APIKeyName         string
	APIKeyPrefix       string
	APIKeySettingsPath string
}

type APIKeyExpirationContext struct {
	APIKeyContext
	ExpireDays int
}

var (
	APIKeyExpirationTemplate = common.NewEmailTemplate("apikey-expiration", apiKeyExpirationHTMLTemplate, apiKeyExpirationTextTemplate, emailFuncs)
	APIKeyExpiredTemplate    = common.NewEmailTemplate("apikey-expired", apiKeyExpiredHTMLTemplate, apiKeyExpiredTextTemplate, emailFuncs)
)

const (
	apiKeyExpirationHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
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
              Your Private Captcha API key <i>"{{.APIKeyName}}"</i> (<code style="background-color:#eee; padding: 1px 2px; border-radius: 2px;">{{.APIKeyPrefix}}...</code>) will expire in {{.ExpireDays}} days or less.
            </p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">You can create a new one or rotate it in the <a href="{{.PortalURL}}/{{.APIKeySettingsPath}}">account settings</a>.</p>
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
	apiKeyExpirationTextTemplate = `Hello,

Your Private Captcha API key "{{.APIKeyName}}" ({{.APIKeyPrefix}}...) will expire in {{.ExpireDays}} days or less.

You can create a new one or rotate it in the account settings ({{.PortalURL}}/{{.APIKeySettingsPath}}).

Warmly,
The Private Captcha team

--

PrivateCaptcha © {{.CurrentYear}} Intmaker OÜ`

	apiKeyExpiredHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
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
              Your Private Captcha API key <i>"{{.APIKeyName}}"</i> (<code style="background-color:#eee; padding: 1px 2px; border-radius: 2px;">{{.APIKeyPrefix}}...</code>) has just expired.
            </p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">You can create a new one in the <a href="{{.PortalURL}}/{{.APIKeySettingsPath}}">account settings</a>.</p>
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

	apiKeyExpiredTextTemplate = `Hello,

Your Private Captcha API key "{{.APIKeyName}}" ({{.APIKeyPrefix}}...) has just expired.

You can create a new one in the account settings ({{.PortalURL}}/{{.APIKeySettingsPath}}).

Warmly,
The Private Captcha team

--

PrivateCaptcha © {{.CurrentYear}} Intmaker OÜ
`
)
