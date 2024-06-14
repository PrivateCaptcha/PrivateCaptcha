package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/badoux/checkmail"
)

var (
	errIncompleteSession = errors.New("data in session is incomplete")
)

const (
	registerFormTemplate = "register/form.html"
)

type registerRenderContext struct {
	Token      string
	NameError  string
	EmailError string
}

func (s *Server) getRegister(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	return &registerRenderContext{
		Token: s.XSRF.Token("", actionRegister),
	}, "register/register.html", nil
}

func (s *Server) postRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormSizeBytes)
	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	data := &registerRenderContext{
		Token: s.XSRF.Token("", actionRegister),
	}

	name := strings.TrimSpace(r.FormValue(common.ParamName))
	if len(name) < 3 {
		data.NameError = "Please use a longer name."
		s.render(w, r, registerFormTemplate, data)
		return
	}

	email := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if err := checkmail.ValidateFormat(email); err != nil {
		slog.Warn("Failed to validate email format", common.ErrAttr(err))
		data.EmailError = "Email address is not valid."
		s.render(w, r, registerFormTemplate, data)
		return
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, "", actionRegister) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		common.Redirect(s.relURL(common.RegisterEndpoint), w, r)
		return
	}

	if _, err := s.Store.FindUser(ctx, email); err == nil {
		slog.WarnContext(ctx, "User with such email already exists", "email", email)
		data.EmailError = "Such email is already registered. Login instead?"
		s.render(w, r, registerFormTemplate, data)
		return
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	sess := s.Session.SessionStart(w, r)
	sess.Set(session.KeyLoginStep, loginStepSignUpVerify)
	sess.Set(session.KeyUserEmail, email)
	sess.Set(session.KeyUserName, name)
	sess.Set(session.KeyTwoFactorCode, code)

	common.Redirect(s.relURL(common.TwoFactorEndpoint), w, r)
}

func (s *Server) doRegister(ctx context.Context, sess *common.Session) error {
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		return errIncompleteSession
	}

	name, ok := sess.Get(session.KeyUserName).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get user name from session")
		return errIncompleteSession
	}

	orgName := common.OrgNameFromName(name)

	_, _, err := s.Store.CreateNewAccount(ctx, nil /*subscription*/, email, name, orgName, -1 /*existing user ID*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create user account in Store", common.ErrAttr(err))
		return err
	}

	return nil
}
