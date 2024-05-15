package portal

import (
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/badoux/checkmail"
)

const (
	maxUserFormSizeBytes    = 256 * 1024
	settingsGeneralTemplate = "settings/general.html"
	settingsTemplate        = "settings/settings.html"
)

type settingsGeneralRenderContext struct {
	Token      string
	Name       string
	NameError  string
	Email      string
	EmailError string
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

func (s *Server) putGeneralSettings(w http.ResponseWriter, r *http.Request) {
	const formTemplate = "settings/general-form.html"

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

	renderCtx := &settingsGeneralRenderContext{
		Token: s.XSRF.Token(email, actionUserSettings),
		Name:  user.Name,
		Email: email,
	}

	name := r.FormValue(common.ParamName)
	if len(name) < 3 {
		renderCtx.NameError = "Please use a longer name."
		s.render(w, r, formTemplate, renderCtx)
		return
	}

	newEmail := r.FormValue(common.ParamEmail)
	if err := checkmail.ValidateFormat(newEmail); err != nil {
		slog.Warn("Failed to validate email format", common.ErrAttr(err))
		renderCtx.EmailError = "Email address is not valid."
		s.render(w, r, formTemplate, renderCtx)
		return
	}

	s.render(w, r, formTemplate, renderCtx)
}
