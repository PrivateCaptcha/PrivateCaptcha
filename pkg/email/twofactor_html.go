package email

const (
	TwoFactorHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html dir="ltr" lang="en">
  <head>
    <link rel="preload" as="image" href="{{.CDN}}/portal/img/pc-logo-light.png" />
    <meta content="text/html; charset=UTF-8" http-equiv="Content-Type" />
    <meta name="x-apple-disable-message-reformatting" />
  </head>
  <body style="background-color:#fff;color:#212121">
    <table align="center" width="100%" border="0" cellpadding="0" cellspacing="0" role="presentation"
      style="max-width:37.5em;padding:20px;margin:0 auto;background-color:#f3f4f6">
      <tbody>
        <tr style="width:100%">
          <td>
            <table align="center" width="100%" border="0" cellpadding="0" cellspacing="0" role="presentation" style="background-color:#fff">
              <tbody>
                <tr>
                  <td>
                    <table align="center" width="100%" border="0" cellpadding="0" cellspacing="0" role="presentation"
                      style="background-color:#1f2937;display:flex;padding:20px 0;align-items:center;justify-content:center">
                      <tbody>
                        <tr>
                          <td>
                            <img alt="PrivateCaptcha's Logo" height="50" src="{{.CDN}}/portal/img/pc-logo-light.png"
                              style="display:block;outline:none;border:none;text-decoration:none;color:#fff"
                              width="100%"/>
                          </td>
                        </tr>
                      </tbody>
                    </table>
                    <table align="center" width="100%" border="0" cellpadding="0" cellspacing="0" role="presentation" style="padding:25px 35px">
                      <tbody>
                        <tr>
                          <td>
                            <h1 style="color:#1f2937;font-family:-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'Fira Sans', 'Droid Sans', 'Helvetica Neue', sans-serif;font-size:20px;font-weight:bold;margin-bottom:15px">
                              Your verification code
                            </h1>
                            <p style="font-size:14px;line-height:24px;margin:24px 0;color:#1f2937;font-family:-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'Fira Sans', 'Droid Sans', 'Helvetica Neue', sans-serif;margin-bottom:14px">
                              We want to make sure it's really you. Please enter the following verification code when prompted.
                            </p>
                            <table align="center" width="100%" border="0" cellpadding="0" cellspacing="0" role="presentation"
                              style="display:flex;align-items:center;justify-content:center">
                              <tbody>
                                <tr>
                                  <td>
                                    <p style="font-size:36px;line-height:24px;margin:10px 0;color:#1f2937;font-family:-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'Fira Sans', 'Droid Sans', 'Helvetica Neue', sans-serif;font-weight:bold;text-align:center">
                                      {{.Code}}
                                    </p>
                                    <p style="font-size:14px;line-height:24px;margin:0px;color:#1f2937;font-family:-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'Fira Sans', 'Droid Sans', 'Helvetica Neue', sans-serif;text-align:center">
                                      (This code is valid for 10 minutes)
                                    </p>
                                  </td>
                                </tr>
                              </tbody>
                            </table>
                          </td>
                        </tr>
                      </tbody>
                    </table>
                  </td>
                </tr>
              </tbody>
            </table>
            <p style="font-size:12px;margin:24px 0 0 0;color:#1f2937;font-family:-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'Fira Sans', 'Droid Sans', 'Helvetica Neue', sans-serif;padding:0 20px">
              Your are receiving this message because the action you are taking requires a verification.
            </p>
            <p style="font-size:12px;color:#1f2937;font-family:-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'Fira Sans', 'Droid Sans', 'Helvetica Neue', sans-serif;padding:0 20px"><a href="{{.Domain}}" style="text-decoration:underline;color:#1f2937;">PrivateCaptcha</a> © {{.CurrentYear}} Intmaker OÜ</p>
          </td>
        </tr>
      </tbody>
    </table>
  </body>
</html>
`
	twoFactorTextTemplate = `
Your verification code

We want to make sure it's really you. Please enter the following verification code when prompted.

{{.Code}}

(This code is valid for 10 minutes)

--------------------------------------------------------------------------------

Your are receiving this message because the action you are taking requires a verification.

PrivateCaptcha © {{.CurrentYear}} Intmaker OÜ

{{.Domain}}
`
)
