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
	errInvalidEmail = errors.New("email is not valid")
)

type PortalEmailVerifier struct{}

var _ common.EmailVerifier = (*PortalEmailVerifier)(nil)

func (ev *PortalEmailVerifier) VerifyEmail(ctx context.Context, email string) error {
	return checkmail.ValidateFormat(email)
}

type PortalMailer struct {
	Mailer                     emailpkg.Sender
	CDNURL                     string
	PortalURL                  string
	EmailFrom                  common.ConfigItem
	AdminEmail                 common.ConfigItem
	ReplyToEmail               common.ConfigItem
	TwofactorTemplate          *common.EmailTemplate
	WelcomeTemplate            *common.EmailTemplate
	OrgInviteItemplate         *common.EmailTemplate
	OrgRegisterInviteTemplate  *common.EmailTemplate
	uaParser                   *useragent.Parser
}

func NewPortalMailer(cdnURL, portalURL string, mailer emailpkg.Sender, cfg common.ConfigStore) *PortalMailer {
	return &PortalMailer{
		Mailer:                     mailer,
		EmailFrom:                  cfg.Get(common.EmailFromKey),
		AdminEmail:                 cfg.Get(common.AdminEmailKey),
		ReplyToEmail:               cfg.Get(common.ReplyToEmailKey),
		CDNURL:                     strings.TrimSuffix(cdnURL, "/"),
		PortalURL:                  strings.TrimSuffix(portalURL, "/"),
		TwofactorTemplate:          emailpkg.TwoFactorEmailTemplate,
		WelcomeTemplate:            emailpkg.WelcomeEmailTemplate,
		OrgInviteItemplate:         emailpkg.OrgInvitationTemplate,
		OrgRegisterInviteTemplate:  emailpkg.OrgRegisterInvitationTemplate,
		uaParser:                   useragent.NewParser(),
	}
}

var _ common.Mailer = (*PortalMailer)(nil)

func (pm *PortalMailer) SendTwoFactor(ctx context.Context, email string, code int, userAgent string, location string) error {
	if len(email) == 0 {
		return errInvalidEmail
	}

	agent := pm.uaParser.Parse(userAgent)
	tnow := time.Now()

	data := &emailpkg.TwoFactorEmailContext{
		Code:        fmt.Sprintf("%06d", code),
		CDNURL:      pm.CDNURL,
		PortalURL:   pm.PortalURL,
		CurrentYear: tnow.Year(),
		Date:        tnow.Format("02 Jan 2006 15:04:05 MST"),
		Browser:     fmt.Sprintf("%s %s", agent.Browser().String(), agent.BrowserVersion()),
		OS:          agent.OS().String(),
		Location:    location,
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
		HTMLBody:  htmlBody,
		TextBody:  textBody,
		Subject:   fmt.Sprintf("[%s] Your verification code is %v", common.PrivateCaptcha, data.Code),
		EmailTo:   email,
		EmailFrom: pm.EmailFrom.Value(),
		NameFrom:  common.PrivateCaptchaTeam,
		ReplyTo:   pm.ReplyToEmail.Value(),
	}

	clog := slog.With("email", email, "code", data.Code)

	if err := pm.Mailer.SendEmail(ctx, msg); err != nil {
		level := slog.LevelError

		if email == pm.AdminEmail.Value() {
			level = slog.LevelWarn
			err = nil
		}

		clog.Log(ctx, level, "Failed to send two factor code", common.ErrAttr(err))

		return err
	}

	clog.InfoContext(ctx, "Sent two factor code")

	return nil
}

func (pm *PortalMailer) SendWelcome(ctx context.Context, email, name string) error {
	data := struct {
		PortalURL   string
		CurrentYear int
		CDNURL      string
		UserName    string
	}{
		CDNURL:      pm.CDNURL,
		PortalURL:   pm.PortalURL,
		CurrentYear: time.Now().Year(),
		UserName:    name,
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
		HTMLBody:  htmlBody,
		TextBody:  textBody,
		Subject:   "Welcome to Private Captcha",
		EmailTo:   email,
		EmailFrom: pm.EmailFrom.Value(),
		NameFrom:  common.PrivateCaptchaTeam,
		ReplyTo:   pm.ReplyToEmail.Value(),
	}

	if err := pm.Mailer.SendEmail(ctx, msg); err != nil {
		slog.ErrorContext(ctx, "Failed to send welcome email", common.ErrAttr(err))

		return err
	}

	slog.InfoContext(ctx, "Sent welcome email", "email", email)

	return nil
}

func (pm *PortalMailer) SendOrgInvite(ctx context.Context, email, name string, orgName, orgOwnerEmail, orgOwnerName, orgURLPath string) error {
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
			UserName:      name,
			OrgName:       orgName,
			OrgOwnerName:  orgOwnerName,
			OrgOwnerEmail: orgOwnerEmail,
			OrgURL:        pm.PortalURL + orgURLPath,
		},
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
		HTMLBody:  htmlBody,
		TextBody:  textBody,
		Subject:   fmt.Sprintf("[%s] You have been invited to the %s organization", common.PrivateCaptcha, data.OrgName),
		EmailTo:   email,
		EmailFrom: pm.EmailFrom.Value(),
		NameFrom:  common.PrivateCaptchaTeam,
	}

	olog := slog.With("email", email, "org", orgName)

	if err := pm.Mailer.SendEmail(ctx, msg); err != nil {
		olog.ErrorContext(ctx, "Failed to send org invite", common.ErrAttr(err))

		return err
	}

	olog.InfoContext(ctx, "Sent org invite")

	return nil
}

func (pm *PortalMailer) SendOrgRegisterInvite(ctx context.Context, email string, orgName, orgOwnerEmail, orgOwnerName, registerURLPath string) error {
	if len(email) == 0 {
		return errInvalidEmail
	}

	data := struct {
		emailpkg.OrgRegisterInvitationContext
		CurrentYear int
		CDNURL      string
	}{
		CDNURL:      pm.CDNURL,
		CurrentYear: time.Now().Year(),
		OrgRegisterInvitationContext: emailpkg.OrgRegisterInvitationContext{
			OrgName:       orgName,
			OrgOwnerName:  orgOwnerName,
			OrgOwnerEmail: orgOwnerEmail,
			RegisterURL:   pm.PortalURL + registerURLPath,
		},
	}

	htmlBody, err := pm.OrgRegisterInviteTemplate.RenderHTML(ctx, data)
	if err != nil {
		return err
	}

	textBody, err := pm.OrgRegisterInviteTemplate.RenderText(ctx, data)
	if err != nil {
		return err
	}

	msg := &emailpkg.Message{
		HTMLBody:  htmlBody,
		TextBody:  textBody,
		Subject:   fmt.Sprintf("[%s] You have been invited to the %s organization", common.PrivateCaptcha, data.OrgName),
		EmailTo:   email,
		EmailFrom: pm.EmailFrom.Value(),
		NameFrom:  common.PrivateCaptchaTeam,
	}

	olog := slog.With("email", email, "org", orgName)

	if err := pm.Mailer.SendEmail(ctx, msg); err != nil {
		olog.ErrorContext(ctx, "Failed to send org register invite", common.ErrAttr(err))

		return err
	}

	olog.InfoContext(ctx, "Sent org register invite")

	return nil
}
