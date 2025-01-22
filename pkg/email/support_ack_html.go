package email

const (
	SupportAcknowledgeHTMLTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
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
      style="max-width:100%;margin:0 auto;padding:20px 0 48px;width:580px"
    >
      <tbody>
        <tr style="width:100%">
          <td>
            <table
              align="center"
              width="100%"
              border="0"
              cellpadding="0"
              cellspacing="0"
              role="presentation"
            >
              <tbody>
                <tr>
                  <td>
                    <img alt="Private Captcha" height="30" src="{{.CDN}}/portal/img/pc-logo-dark.png" style="display:block;outline:none;border:none;text-decoration:none" />
                  </td>
                </tr>
              </tbody>
            </table>
            <table
              align="center"
              width="100%"
              border="0"
              cellpadding="0"
              cellspacing="0"
              role="presentation"
            >
              <tbody>
                <tr>
                  <td>
                    <table
                      align="center"
                      width="100%"
                      border="0"
                      cellpadding="0"
                      cellspacing="0"
                      role="presentation"
                    >
                      <tbody style="width:100%">
                        <tr style="width:100%">
                          <p style="font-size:18px;line-height:1.3;margin:16px 0;font-weight:700;color:#231f20">
                            We have received your support request
                          </p>
                          <p style="font-size:16px;line-height:1.4;margin:16px 0;color:#231f20;">This is an automatic notification to let you know that we've got your ticket. Our support team will get back to you shortly.</p>
                          <p style="font-size:16px;line-height:1.4;margin:16px 0;color:#231f20;">Please do not reply to this email. If you wish to tell us more about your issue, please create a new ticket with more details.</p>
                        </tr>
                      </tbody>
                    </table>
                  </td>
                </tr>
              </tbody>
            </table>
            <hr style="width:100%;border:none;border-top:1px solid #eaeaea;border-color:#cccccc;margin:20px 0" />
            <table
              align="center"
              width="100%"
              border="0"
              cellpadding="0"
              cellspacing="0"
              role="presentation"
            >
              <tbody>
                <tr>
                  <td>
                    <table
                      align="center"
                      width="100%"
                      border="0"
                      cellpadding="0"
                      cellspacing="0"
                      role="presentation"
                    >
                      <tbody style="width:100%">
                        <tr style="width:100%">
                          <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
                              Ticket ID: {{.TicketID}}
                          </p>
                          <p style="font-size:14px;line-height:24px;margin:16px 0;color:#9ca299;margin-bottom:10px">
                              <a href="{{.Domain}}" style="text-decoration:underline;color:#9ca299;">PrivateCaptcha</a> © {{.CurrentYear}} Intmaker OÜ
                          </p>
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
  </body>
</html>`
	supportAcknowledgeTextTemplate = `
We have received your support request

This is an automatic notification to let you know that we've got your ticket. Our support team will get back to you shortly.

Please do not reply to this email. If you wish to tell us more about your issue, please create a new ticket with more details.

--------------------------------------------------------------------------------

Ticket ID: {{.TicketID}}

PrivateCaptcha © {{.CurrentYear}} Intmaker OÜ
`
)
