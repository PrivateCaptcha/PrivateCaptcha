package portal

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/badoux/checkmail"
)

const (
	maxUserFormSizeBytes    = 256 * 1024
	settingsGeneralTemplate = "settings/general.html"
	settingsTemplate        = "settings/settings.html"

	settingsGeneralFormTemplate = "settings/general-form.html"
)

type settingsGeneralRenderContext struct {
	Token          string
	Name           string
	NameError      string
	Email          string
	EmailError     string
	TwoFactorError string
	TwoFactorEmail string
	UpdateMessage  string
	EditEmail      bool
}

func (s *Server) getGeneralSettings(tpl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sess := s.Session.SessionStart(w, r)
		email, ok := sess.Get(session.KeyUserEmail).(string)
		if !ok {
			slog.ErrorContext(ctx, "Failed to get email from session")
			common.Redirect(s.relURL(common.LoginEndpoint), w, r)
			return
		}

		user, err := s.Store.FindUser(ctx, email)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
			s.redirectError(http.StatusInternalServerError, w, r)
			return
		}

		renderCtx := &settingsGeneralRenderContext{
			Token: s.XSRF.Token(email, actionUserSettings),
			Name:  user.Name,
			Email: email,
		}

		s.render(w, r, tpl, renderCtx)
	}
}

func (s *Server) editEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	sess.Set(session.KeyTwoFactorCode, code)

	renderCtx := &settingsGeneralRenderContext{
		Token:          s.XSRF.Token(email, actionUserSettings),
		Name:           user.Name,
		Email:          user.Email,
		TwoFactorEmail: common.MaskEmail(email, '*'),
		EditEmail:      true,
	}

	s.render(w, r, settingsGeneralFormTemplate, renderCtx)
}

func (s *Server) putGeneralSettings(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUserFormSizeBytes)
	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, email, actionUserSettings) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	formName := r.FormValue(common.ParamName)
	formEmail := strings.TrimSpace(r.FormValue(common.ParamEmail))

	renderCtx := &settingsGeneralRenderContext{
		Token:          s.XSRF.Token(email, actionUserSettings),
		Name:           formName,
		Email:          formEmail,
		TwoFactorEmail: common.MaskEmail(email, '*'),
	}

	if len(formName) < 3 {
		renderCtx.NameError = "Please use a longer name."
		s.render(w, r, settingsGeneralFormTemplate, renderCtx)
		return
	}

	// anyChange := formName != user.Name

	if formEmail != user.Email {
		if err := checkmail.ValidateFormat(formEmail); err != nil {
			slog.Warn("Failed to validate email format", common.ErrAttr(err))
			renderCtx.EmailError = "Email address is not valid."
			s.render(w, r, settingsGeneralFormTemplate, renderCtx)
			return
		}

		sentCode, hasSent2FA := sess.Get(session.KeyTwoFactorCode).(int)
		formCode := r.FormValue(common.ParamVerificationCode)

		if (len(formCode) == 0) || !hasSent2FA {

			//renderCtx.Waiting2FA = true
			renderCtx.Name = formName
		} else if enteredCode, err := strconv.Atoi(formCode); (err != nil) || (enteredCode != sentCode) {
			slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "expected", sentCode, common.ErrAttr(err))
			renderCtx.TwoFactorError = "Code is not valid."
			s.render(w, r, settingsGeneralFormTemplate, renderCtx)
			return
		} else {
			//anyChange = true
		}
	}

	s.render(w, r, settingsGeneralFormTemplate, renderCtx)
}
