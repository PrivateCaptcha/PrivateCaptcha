package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/badoux/checkmail"
)

const (
	maxUserFormSizeBytes        = 256 * 1024
	settingsGeneralTemplate     = "settings/general.html"
	settingsTemplate            = "settings/settings.html"
	settingsGeneralFormTemplate = "settings/general-form.html"
	settingsAPIKeysTemplate     = "settings/apikeys.html"
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
	UpdateError    string
	EditEmail      bool
}

type userAPIKey struct {
	ID          string
	Name        string
	ExpiresAt   string
	Secret      string
	ExpiresSoon bool
}

type settingsAPIKeysRenderContext struct {
	Name       string
	NameError  string
	Keys       []*userAPIKey
	Token      string
	CreateOpen bool
}

func apiKeyToUserAPIKey(key *dbgen.APIKey, tnow time.Time) *userAPIKey {
	return &userAPIKey{
		ID:          strconv.Itoa(int(key.ID)),
		Name:        key.Name,
		ExpiresAt:   key.ExpiresAt.Time.Format("02 Jan 2006"),
		ExpiresSoon: key.ExpiresAt.Time.Sub(tnow) < 31*24*time.Hour,
	}
}

func apiKeysToUserAPIKeys(keys []*dbgen.APIKey, tnow time.Time) []*userAPIKey {
	result := make([]*userAPIKey, 0, len(keys))

	for _, key := range keys {
		result = append(result, apiKeyToUserAPIKey(key, tnow))
	}

	return result
}

func (s *Server) getGeneralSettings(tpl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.sessionUser(w, r)
		if err != nil {
			return
		}

		renderCtx := &settingsGeneralRenderContext{
			Token: s.XSRF.Token(user.Email, actionUserSettings),
			Name:  user.Name,
			Email: user.Email,
		}

		s.render(w, r, tpl, renderCtx)
	}
}

func (s *Server) editEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(w, r)
	if err != nil {
		return
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	sess, ok := ctx.Value(common.SessionContextKey).(*common.Session)
	if !ok {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
	}
	sess.Set(session.KeyTwoFactorCode, code)

	renderCtx := &settingsGeneralRenderContext{
		Token:          s.XSRF.Token(user.Email, actionUserSettings),
		Name:           user.Name,
		Email:          user.Email,
		TwoFactorEmail: common.MaskEmail(user.Email, '*'),
		EditEmail:      true,
	}

	s.render(w, r, settingsGeneralFormTemplate, renderCtx)
}

func (s *Server) putGeneralSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.sessionUser(w, r)
	if err != nil {
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
	if !s.XSRF.VerifyToken(token, user.Email, actionUserSettings) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	formName := strings.TrimSpace(r.FormValue(common.ParamName))
	formEmail := strings.TrimSpace(r.FormValue(common.ParamEmail))

	renderCtx := &settingsGeneralRenderContext{
		Token:     s.XSRF.Token(user.Email, actionUserSettings),
		Name:      user.Name,
		Email:     user.Email,
		EditEmail: (len(formEmail) > 0) && (formEmail != user.Email) && ((len(formName) == 0) || (formName == user.Name)),
	}

	anyChange := false
	sess := s.session(w, r)

	if renderCtx.EditEmail {
		renderCtx.Email = formEmail
		renderCtx.TwoFactorEmail = common.MaskEmail(user.Email, '*')

		if err := checkmail.ValidateFormat(formEmail); err != nil {
			slog.Warn("Failed to validate email format", common.ErrAttr(err))
			renderCtx.EmailError = "Email address is not valid."
			s.render(w, r, settingsGeneralFormTemplate, renderCtx)
			return
		}

		sentCode, hasSentCode := sess.Get(session.KeyTwoFactorCode).(int)
		formCode := r.FormValue(common.ParamVerificationCode)

		// we "used" the code now
		sess.Delete(session.KeyTwoFactorCode)

		if enteredCode, err := strconv.Atoi(formCode); !hasSentCode || (err != nil) || (enteredCode != sentCode) {
			slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "expected", sentCode, common.ErrAttr(err))
			renderCtx.TwoFactorError = "Code is not valid."
			s.render(w, r, settingsGeneralFormTemplate, renderCtx)
			return
		}

		anyChange = (len(formEmail) > 0) && (formEmail != user.Email)
	} else /*edit name only*/ {
		renderCtx.Name = formName

		if (formName != user.Name) && (len(formName) > 0) && (len(formName) < 3) {
			renderCtx.NameError = "Please use a longer name."
			s.render(w, r, settingsGeneralFormTemplate, renderCtx)
			return
		}

		anyChange = (len(formName) > 0) && (formName != user.Name)
	}

	if anyChange {
		if err := s.Store.UpdateUser(ctx, user.ID, renderCtx.Name, renderCtx.Email /*new email*/, user.Email /*old email*/); err == nil {
			renderCtx.UpdateMessage = "Settings were updated."
			renderCtx.EditEmail = false
			sess.Set(session.KeyUserEmail, renderCtx.Email)
			sess.Set(session.KeyUserName, renderCtx.Name)
		} else {
			renderCtx.UpdateError = "Failed to update settings. Please try again."
		}
	}

	s.render(w, r, settingsGeneralFormTemplate, renderCtx)
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(w, r)
	if err != nil {
		return
	}

	if err := s.Store.SoftDeleteUser(ctx, user.ID, user.Email); err == nil {
		// TODO: Cancel subscription if any
		s.logout(w, r)
	} else {
		slog.ErrorContext(ctx, "Failed to delete user", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
	}
}

func (s *Server) getAPIKeysSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(w, r)
	if err != nil {
		return
	}

	keys, err := s.Store.RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &settingsAPIKeysRenderContext{
		Keys:  apiKeysToUserAPIKeys(keys, time.Now().UTC()),
		Token: s.XSRF.Token(user.Email, actionAPIKeysSettings),
	}

	s.render(w, r, settingsAPIKeysTemplate, renderCtx)
}

func monthsFromParam(ctx context.Context, param string) int {
	i, err := strconv.Atoi(param)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert months", "value", param, common.ErrAttr(err))
		return 12
	}

	switch i {
	case 3, 6, 12:
		return i
	default:
		return 12
	}
}

func (s *Server) postAPIKeySettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(w, r)
	if err != nil {
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
	if !s.XSRF.VerifyToken(token, user.Email, actionAPIKeysSettings) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		common.Redirect(s.relURL(common.ExpiredEndpoint), w, r)
		return
	}

	keys, err := s.Store.RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &settingsAPIKeysRenderContext{
		Keys:  apiKeysToUserAPIKeys(keys, time.Now().UTC()),
		Token: s.XSRF.Token(user.Email, actionAPIKeysSettings),
	}

	formName := strings.TrimSpace(r.FormValue(common.ParamName))
	if len(formName) < 3 {
		renderCtx.NameError = "Name is too short."
		renderCtx.CreateOpen = true
		s.render(w, r, settingsAPIKeysTemplate, renderCtx)
		return
	}

	months := monthsFromParam(ctx, r.FormValue(common.ParamMonths))
	tnow := time.Now().UTC()
	expiration := tnow.AddDate(0, months, 0)
	key, err := s.Store.CreateAPIKey(ctx, user.ID, formName, expiration)
	if err == nil {
		userKey := apiKeyToUserAPIKey(key, tnow)
		userKey.Secret = db.UUIDToSecret(key.ExternalID)
		renderCtx.Keys = append(renderCtx.Keys, userKey)
	} else {
		slog.ErrorContext(ctx, "Failed to create API key", common.ErrAttr(err))
		// TODO: show error message
	}

	s.render(w, r, settingsAPIKeysTemplate, renderCtx)
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(w, r)
	if err != nil {
		return
	}

	keyID := ctx.Value(common.KeyIDContextKey).(int)

	if err := s.Store.SoftDeleteAPIKey(ctx, user.ID, int32(keyID)); err != nil {
		slog.ErrorContext(ctx, "Failed to soft-delete the key", "keyID", keyID, common.ErrAttr(err))
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
}
