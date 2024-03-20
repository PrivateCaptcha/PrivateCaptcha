package portal

import (
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		data := struct {
			Token     string
			Prefix    string
			TokenName string
		}{
			Token:     s.XSRF.Token("", actionLogin),
			Prefix:    s.Prefix,
			TokenName: common.ParamCsrfToken,
		}

		s.render(ctx, w, "login/login.html", data)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormSizeBytes)
		err := r.ParseForm()
		if err != nil {
			slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
			s.renderError(ctx, w, http.StatusBadRequest)
			return
		}

		token := r.FormValue(common.ParamCsrfToken)
		if !s.XSRF.VerifyToken(token, "", actionLogin) {
			slog.WarnContext(ctx, "Failed to verify CSRF token")
			s.renderError(ctx, w, http.StatusUnauthorized)
			return
		}

		email := r.FormValue(common.ParamEmail)
		user, err := s.Store.FindUser(ctx, email)
		if err != nil {
			data := struct {
				Error string
			}{
				Error: "User with such email does not exist",
			}

			s.render(ctx, w, "login/email-error.html", data)
			return
		}

		sess := s.Session.SessionStart(w, r)
		sess.Set(session.KeyLoginStep, loginStepTwoFactor)
		sess.Set(session.KeyUserEmail, user.Email)
		code := twoFactorCode()
		sess.Set(session.KeyTwoFactorCode, code)

		if err := s.Mailer.SendTwoFactor(ctx, user.Email, code); err != nil {
			slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
			s.renderError(ctx, w, http.StatusInternalServerError)
			return
		}

		s.htmxRedirect(s.relURL(common.TwoFactorEndpoint), w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) twofactor(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sess := s.Session.SessionStart(w, r)
		if step, ok := sess.Get(session.KeyLoginStep).(int); !ok || step != loginStepTwoFactor {
			common.Redirect(s.relURL(common.LoginEndpoint), w, r)
			return
		}
	case http.MethodPost:
		// TODO: Verify two factor code stored in session
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
	}
}
