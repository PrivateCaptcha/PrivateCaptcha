package email

import "github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"

var (
	WelcomeEmailTemplate = common.NewEmailTemplate("welcome", welcomeHTMLTemplate, welcomeTextTemplate, emailFuncs)
)

const (
	welcomeHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html dir="ltr" lang="en">
  <head>
    <link rel="preload" as="image" href="{{.CDNURL}}/portal/img/pc-logo-dark.png" />
    <meta content="text/html; charset=UTF-8" http-equiv="Content-Type" />
    <meta name="x-apple-disable-message-reformatting" />
  </head>
  <body
    style='background-color:#ffffff;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Oxygen-Sans,Ubuntu,Cantarell,"Helvetica Neue",sans-serif'
  >
    {{ $utm := .UTM | default "utm_medium=email&utm_source=welcome" }}
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
              Hello{{if .UserName}} {{.UserName}}{{end}},
            </p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">
              Welcome to Private Captcha, a privacy- and user-friendly protection from bots and spam.
            </p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">If this is your first time integrating captcha, our <a href="https://docs.privatecaptcha.com/docs/tutorials/complete-example/?{{$utm}}">example tutorial</a> will help to learn how it works end-to-end.</p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">For those migrating from Google reCAPTCHA or similar services, our <a href="https://docs.privatecaptcha.com/docs/tutorials/migrate-from-recaptcha/?{{$utm}}">migration guide</a> can be useful.</p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">Already familiar with all this? The <a href="https://docs.privatecaptcha.com/docs/reference/?{{$utm}}">reference docs</a> are ready whenever you are. And of course, our team is here to help if you have any questions.</p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">
              Warmly,<br />The Private Captcha team
            </p>
            <hr style="width:100%;border:none;border-top:1px solid #eaeaea;border-color:#cccccc;margin:20px 0" />
            <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
            <a href="https://privatecaptcha.com?{{$utm}}" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> © {{.CurrentYear}} Intmaker OÜ
            </p>
          </td>
        </tr>
      </tbody>
    </table>
  </body>
</html>`

	welcomeTextTemplate = `Hello{{if .UserName}} {{.UserName}}{{end}},

Welcome to Private Captcha, a privacy- and user-friendly protection from bots and spam.

If this is your first time integrating captcha, our example tutorial (https://docs.privatecaptcha.com/docs/tutorials/complete-example/) will help to learn how it works end-to-end.

For those migrating from Google reCAPTCHA or similar services, our migration guide (https://docs.privatecaptcha.com/docs/tutorials/migrate-from-recaptcha/) can be useful.

Already familiar with all this? The reference docs (https://docs.privatecaptcha.com/docs/reference/) are ready whenever you are. And of course, our team is here to help if you have any questions.


Warmly,
The Private Captcha team

---

PrivateCaptcha © {{.CurrentYear}} Intmaker OÜ`
)
