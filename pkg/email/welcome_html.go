package email

const (
	WelcomeHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html dir="ltr" lang="en">
  <head>
    <link rel="preload" as="image" href="{{.CDN}}/portal/img/pc-logo-dark.png" />
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
            <img alt="Private Captcha" height="50" src="{{.CDN}}/portal/img/pc-logo-dark.png" style="display:block;outline:none;border:none;text-decoration:none" />
            <p style="font-size:16px;line-height:26px;margin:16px 0">
              Hello,
            </p>
            <p style="font-size:16px;line-height:26px;margin:16px 0">
              Welcome to Private Captcha, a privacy- and user-friendly protection from bots and spam.
            </p>
            <table align="center" width="100%" border="0" cellpadding="0" cellspacing="0" role="presentation" style="text-align:center">
              <tbody>
                <tr>
                  <td>
                      <a href="{{.Domain}}" style="line-height:1.5rem;text-decoration:none;display:block;max-width:300px;mso-padding-alt:0px;background-color:#111827;border-radius:0.75rem;color:#fff;font-size:1rem;text-align:center;padding:1rem 2rem;font-weight: 700;"
                      target="_blank"
                      ><span
                        ><!--[if mso
                          ]><i
                            style="mso-font-width:300%;mso-text-raise:18"
                            hidden
                            >&#8202;&#8202;</i
                          ><!
                        [endif]--></span
                      ><span
                        style="max-width:100%;display:inline-block;line-height:120%;mso-padding-alt:0px;mso-text-raise:9px"
                        >Get started</span
                      ><span
                        ><!--[if mso
                          ]><i style="mso-font-width:300%" hidden
                            >&#8202;&#8202;&#8203;</i
                          ><!
                        [endif]--></span
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
                <a href="{{.Domain}}" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> © {{.CurrentYear}} Intmaker OÜ
            </p>
          </td>
        </tr>
      </tbody>
    </table>
  </body>
</html>`

	welcomeTextTemplate = `
Hello,

Welcome to Private Captcha, a privacy- and user-friendly protection from bots and spam.

Get started {{.Domain}}

Warmly,
The Private Captcha team

--------------------------------------------------------------------------------

PrivateCaptcha © {{.CurrentYear}} Intmaker OÜ`
)
