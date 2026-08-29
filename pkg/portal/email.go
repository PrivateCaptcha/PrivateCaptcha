package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/badoux/checkmail"
	"github.com/medama-io/go-useragent"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	emailpkg "github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
)

var (
	errInvalidEmail       = errors.New("email is not valid")
	errInvalidEmailDomain = errors.New("email domain is unlikely valid")
)

type PortalEmailVerifier struct{}

var _ common.EmailVerifier = (*PortalEmailVerifier)(nil)

func (ev *PortalEmailVerifier) VerifyEmail(ctx context.Context, email string) error {
	if err := checkmail.ValidateFormat(email); err != nil {
		return err
	}

	if at := strings.LastIndex(email, "@"); at != -1 {
		domain := email[at+1:]

		if !emailpkg.IsLikelyValidDomain(domain) {
			return errInvalidEmailDomain
		}
	}

	return nil
}

type PortalMailer struct {
	Mailer             emailpkg.Sender
	CDNURL             string
	PortalURL          string
	AdminEmail         common.ConfigItem
	ReplyToEmail       common.ConfigItem
	TwofactorTemplate  *common.EmailTemplate
	WelcomeTemplate    *common.EmailTemplate
	OrgInviteItemplate *common.EmailTemplate
	uaParser           *useragent.Parser
}

func NewPortalMailer(cdnURL, portalURL string, mailer emailpkg.Sender, cfg common.ConfigStore, uaParser *useragent.Parser) *PortalMailer {
	if uaParser == nil {
		uaParser = useragent.NewParser()
	}

	return &PortalMailer{
		Mailer:             mailer,
		AdminEmail:         cfg.Get(common.AdminEmailKey),
		ReplyToEmail:       cfg.Get(common.ReplyToEmailKey),
		CDNURL:             strings.TrimSuffix(cdnURL, "/"),
		PortalURL:          strings.TrimSuffix(portalURL, "/"),
		TwofactorTemplate:  emailpkg.TwoFactorEmailTemplate,
		WelcomeTemplate:    emailpkg.WelcomeEmailTemplate,
		OrgInviteItemplate: emailpkg.OrgInvitationTemplate,
		uaParser:           uaParser,
	}
}

var _ common.Mailer = (*PortalMailer)(nil)

func (pm *PortalMailer) SendTwoFactor(ctx context.Context, email string, code int, userAgent string, location string, isRegistration bool) error {
	if len(email) == 0 {
		return errInvalidEmail
	}

	agent := pm.uaParser.Parse(userAgent)
	tnow := time.Now()
	location = validCountryCode(location)

	data := &emailpkg.TwoFactorEmailContext{
		Code:        fmt.Sprintf("%06d", code),
		CDNURL:      pm.CDNURL,
		PortalURL:   pm.PortalURL,
		CurrentYear: tnow.Year(),
		Date:        tnow.Format("02 Jan 2006 15:04:05 MST"),
		Browser:     fmt.Sprintf("%s %s", agent.Browser().String(), agent.BrowserVersion()),
		OS:          agent.OS().String(),
		Location:    location,
		ShowDetails: !isRegistration,
		UTM:         "utm_medium=email&utm_source=2fa",
	}

	htmlBody, err := pm.TwofactorTemplate.RenderHTML(ctx, data)
	if err != nil {
		return err
	}

	textBody, err := pm.TwofactorTemplate.RenderText(ctx, data)
	if err != nil {
		return err
	}

	msg := &emailpkg.Message{
		HTMLBody: htmlBody,
		TextBody: textBody,
		Subject:  fmt.Sprintf("[%s] Your verification code is %v", common.PrivateCaptcha, data.Code),
		EmailTo:  email,
		NameFrom: common.PrivateCaptchaTeam,
		ReplyTo:  pm.ReplyToEmail.Value(),
	}

	clog := slog.With("email", email)

	if err := pm.Mailer.SendEmail(ctx, msg); err != nil {
		level := slog.LevelError

		if (email == pm.AdminEmail.Value()) || (email == spammerEmail) {
			level = slog.LevelWarn
			err = nil
		}

		clog.Log(ctx, level, "Failed to send two factor code", common.ErrAttr(err))

		return err
	}

	clog.InfoContext(ctx, "Sent two factor code")

	return nil
}

func validCountryCode(value string) string {
	if len(value) != 2 {
		return ""
	}

	value = strings.ToUpper(value)
	if (value[0] < 'A') || (value[0] > 'Z') || (value[1] < 'A') || (value[1] > 'Z') {
		return ""
	}

	return value
}

func (pm *PortalMailer) SendWelcome(ctx context.Context, email, name string) error {
	data := struct {
		PortalURL   string
		CurrentYear int
		CDNURL      string
		UserName    string
		UTM         string
	}{
		CDNURL:      pm.CDNURL,
		PortalURL:   pm.PortalURL,
		CurrentYear: time.Now().Year(),
		UserName:    name,
		UTM:         "utm_medium=email&utm_source=welcome",
	}

	htmlBody, err := pm.WelcomeTemplate.RenderHTML(ctx, data)
	if err != nil {
		return err
	}

	textBody, err := pm.WelcomeTemplate.RenderText(ctx, data)
	if err != nil {
		return err
	}

	msg := &emailpkg.Message{
		HTMLBody: htmlBody,
		TextBody: textBody,
		Subject:  "Welcome to Private Captcha",
		EmailTo:  email,
		NameFrom: common.PrivateCaptchaTeam,
		ReplyTo:  pm.ReplyToEmail.Value(),
	}

	if err := pm.Mailer.SendEmail(ctx, msg); err != nil {
		slog.ErrorContext(ctx, "Failed to send welcome email", common.ErrAttr(err))

		return err
	}

	slog.InfoContext(ctx, "Sent welcome email", "email", email)

	return nil
}

func (pm *PortalMailer) SendOrgInvite(ctx context.Context, email, name string, orgName, orgOwnerEmail, orgOwnerName, orgURLPath string, requiresRegister bool) error {
	if len(email) == 0 {
		return errInvalidEmail
	}

	data := struct {
		emailpkg.OrgInvitationContext
		CurrentYear int
		CDNURL      string
	}{
		CDNURL:      pm.CDNURL,
		CurrentYear: time.Now().Year(),
		OrgInvitationContext: emailpkg.OrgInvitationContext{
			UserName:         name,
			OrgName:          orgName,
			OrgOwnerName:     orgOwnerName,
			OrgOwnerEmail:    orgOwnerEmail,
			OrgURL:           pm.PortalURL + orgURLPath,
			RequiresRegister: requiresRegister,
			UTM:              "utm_medium=email&utm_source=org",
		},
	}

	if strings.Contains(data.OrgURL, "?") {
		data.OrgURL += "&" + data.UTM
	} else {
		data.OrgURL += "?" + data.UTM
	}

	htmlBody, err := pm.OrgInviteItemplate.RenderHTML(ctx, data)
	if err != nil {
		return err
	}

	textBody, err := pm.OrgInviteItemplate.RenderText(ctx, data)
	if err != nil {
		return err
	}

	msg := &emailpkg.Message{
		HTMLBody: htmlBody,
		TextBody: textBody,
		Subject:  fmt.Sprintf("[%s] You have been invited to the %s organization", common.PrivateCaptcha, data.OrgName),
		EmailTo:  email,
		NameFrom: common.PrivateCaptchaTeam,
	}

	olog := slog.With("email", email, "org", orgName)

	if err := pm.Mailer.SendEmail(ctx, msg); err != nil {
		olog.ErrorContext(ctx, "Failed to send org invite", common.ErrAttr(err))

		return err
	}

	olog.InfoContext(ctx, "Sent org invite")

	return nil
}
