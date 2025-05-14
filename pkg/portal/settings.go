package portal

import (
	"context"
	"errors"
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
	// Content-specific template names
	settingsGeneralTemplatePrefix = "settings-general/"
	settingsAPIKeysTemplatePrefix = "settings-apikeys/"
	settingsUsageTemplatePrefix   = "settings-usage/"

	// Other templates
	settingsGeneralFormTemplate    = "settings-general/form.html"
	settingsAPIKeysContentTemplate = "settings-apikeys/content.html"
)

var (
	errNoTabs = errors.New("no settings tabs configured")
)

type SettingsTab struct {
	ID             string
	Name           string
	TemplatePrefix string
	ModelHandler   ModelFunc
}

// SettingsTabViewModel is used for rendering the navigation in templates
type SettingsTabViewModel struct {
	ID           string
	Name         string
	IconTemplate string
	IsActive     bool
}

type SettingsCommonRenderContext struct {
	AlertRenderContext
	CsrfRenderContext

	// For navigation and content rendering
	Tabs        []*SettingsTabViewModel
	ActiveTabID string
	Email       string
	UserID      int32
}

type settingsUsageRenderContext struct {
	SettingsCommonRenderContext
	Limit int
}

type settingsGeneralRenderContext struct {
	SettingsCommonRenderContext
	Name           string
	NameError      string
	EmailError     string
	TwoFactorError string
	TwoFactorEmail string
	EditEmail      bool
}

type userAPIKey struct {
	ID                string
	Name              string
	ExpiresAt         string
	Secret            string
	RequestsPerMinute int
	ExpiresSoon       bool
}

type settingsAPIKeysRenderContext struct {
	SettingsCommonRenderContext
	Name       string
	NameError  string
	Keys       []*userAPIKey
	CreateOpen bool
}

func apiKeyToUserAPIKey(key *dbgen.APIKey, tnow time.Time) *userAPIKey {
	// in terms of "leaky bucket" logic
	capacity := float64(key.RequestsBurst)
	leakInterval := float64(time.Second) / key.RequestsPerSecond
	// {period} during which we can consume (or restore) {capacity}
	period := capacity * leakInterval
	periodsPerMinute := float64(time.Minute) / period
	requestsPerMinute := capacity * periodsPerMinute

	return &userAPIKey{
		ID:                strconv.Itoa(int(key.ID)),
		Name:              key.Name,
		ExpiresAt:         key.ExpiresAt.Time.Format("02 Jan 2006"),
		ExpiresSoon:       key.ExpiresAt.Time.Sub(tnow) < 31*24*time.Hour,
		RequestsPerMinute: int(requestsPerMinute),
	}
}

func apiKeysToUserAPIKeys(keys []*dbgen.APIKey, tnow time.Time) []*userAPIKey {
	result := make([]*userAPIKey, 0, len(keys))

	for _, key := range keys {
		result = append(result, apiKeyToUserAPIKey(key, tnow))
	}

	return result
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	tabParam := r.URL.Query().Get(common.ParamTab)
	slog.Log(ctx, common.LevelTrace, "Settings tab was requested", "tab", tabParam)

	tab, err := s.findTab(ctx, tabParam)
	if err != nil {
		return nil, "", err
	}

	model, _, err := tab.ModelHandler(w, r)
	if err != nil {
		return nil, "", err
	}

	return model, tab.TemplatePrefix + "page.html", nil
}

func (s *Server) getSettingsTab(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	tabID, err := common.StrPathArg(r, common.ParamTab)
	if err != nil {
		slog.ErrorContext(ctx, "Cannot retrieve tab from path", common.ErrAttr(err))
	}

	tab, err := s.findTab(ctx, tabID)
	if err != nil {
		return nil, "", err
	}

	model, _, err := tab.ModelHandler(w, r)
	if err != nil {
		return nil, "", err
	}

	return model, tab.TemplatePrefix + "tab.html", nil
}

func (s *Server) findTab(ctx context.Context, tabID string) (*SettingsTab, error) {
	var tab *SettingsTab

	if len(tabID) > 0 && len(s.SettingsTabs) > 0 {
		for _, t := range s.SettingsTabs {
			if t.ID == tabID {
				tab = t
				break
			}
		}

		if tab == nil {
			slog.ErrorContext(ctx, "Unknown or no active tab found", "tab", tabID)
		}
	}

	if tab == nil {
		if len(s.SettingsTabs) > 0 {
			tab = s.SettingsTabs[0]
		} else {
			slog.ErrorContext(ctx, "Configuration error", common.ErrAttr(errNoTabs))
			return nil, errNoTabs
		}
	}

	return tab, nil
}

func CreateTabViewModels(activeTabID string, tabs []*SettingsTab) []*SettingsTabViewModel {
	viewModels := make([]*SettingsTabViewModel, 0, len(tabs))
	for _, tab := range tabs {
		viewModels = append(viewModels, &SettingsTabViewModel{
			ID:           tab.ID,
			Name:         tab.Name,
			IsActive:     tab.ID == activeTabID,
			IconTemplate: tab.TemplatePrefix + "icon.html",
		})
	}
	return viewModels
}

func (s *Server) CreateSettingsCommonRenderContext(activeTabID string, user *dbgen.User) SettingsCommonRenderContext {
	viewModels := CreateTabViewModels(activeTabID, s.SettingsTabs)

	return SettingsCommonRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		ActiveTabID:       activeTabID,
		Tabs:              viewModels,
		Email:             user.Email,
		UserID:            user.ID,
	}
}

func (s *Server) createGeneralSettingsModel(ctx context.Context, user *dbgen.User) *settingsGeneralRenderContext {
	return &settingsGeneralRenderContext{
		SettingsCommonRenderContext: s.CreateSettingsCommonRenderContext(common.GeneralEndpoint, user),
		Name:                        user.Name,
	}
}

func (s *Server) getGeneralSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := s.createGeneralSettingsModel(ctx, user)

	return renderCtx, "", nil
}

func (s *Server) editEmail(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	sess := s.Session(w, r)
	user, err := s.SessionUser(ctx, sess)
	if err != nil {
		return nil, "", err
	}

	renderCtx := s.createGeneralSettingsModel(ctx, user)
	renderCtx.EditEmail = true
	renderCtx.TwoFactorEmail = common.MaskEmail(user.Email, '*')

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to send verification code. Please try again."
	} else {
		_ = sess.Set(session.KeyTwoFactorCode, code)
	}

	return renderCtx, settingsGeneralFormTemplate, nil
}

func (s *Server) putGeneralSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", ErrInvalidRequestArg
	}

	formName := strings.TrimSpace(r.FormValue(common.ParamName))
	formEmail := strings.TrimSpace(r.FormValue(common.ParamEmail))

	renderCtx := s.createGeneralSettingsModel(ctx, user)
	renderCtx.EditEmail = (len(formEmail) > 0) && (formEmail != user.Email) && ((len(formName) == 0) || (formName == user.Name))

	anyChange := false
	sess := s.Session(w, r)

	if renderCtx.EditEmail {
		renderCtx.Email = formEmail
		renderCtx.TwoFactorEmail = common.MaskEmail(user.Email, '*')

		if err := checkmail.ValidateFormat(formEmail); err != nil {
			slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
			renderCtx.EmailError = "Email address is not valid."
			return renderCtx, settingsGeneralFormTemplate, nil
		}

		sentCode, hasSentCode := sess.Get(session.KeyTwoFactorCode).(int)
		formCode := r.FormValue(common.ParamVerificationCode)

		// we "used" the code now
		_ = sess.Delete(session.KeyTwoFactorCode)

		if enteredCode, err := strconv.Atoi(formCode); !hasSentCode || (err != nil) || (enteredCode != sentCode) {
			slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "expected", sentCode, common.ErrAttr(err))
			renderCtx.TwoFactorError = "Code is not valid."
			return renderCtx, settingsGeneralFormTemplate, nil
		}

		anyChange = (len(formEmail) > 0) && (formEmail != user.Email)
	} else /*edit name only*/ {
		renderCtx.Name = formName

		if (formName != user.Name) && (len(formName) > 0) && (len(formName) < 3) {
			renderCtx.NameError = "Please use a longer name."
			return renderCtx, settingsGeneralFormTemplate, nil
		}

		anyChange = (len(formName) > 0) && (formName != user.Name)
	}

	if anyChange {
		emailToUpdate := user.Email
		if renderCtx.EditEmail {
			emailToUpdate = formEmail
		}
		if err := s.Store.UpdateUser(ctx, user.ID, renderCtx.Name, emailToUpdate, user.Email); err == nil {
			renderCtx.SuccessMessage = "Settings were updated."
			renderCtx.EditEmail = false
			_ = sess.Set(session.KeyUserName, renderCtx.Name)
			if emailToUpdate != user.Email {
				_ = sess.Set(session.KeyUserEmail, emailToUpdate)
				renderCtx.Email = emailToUpdate
			}
		} else {
			renderCtx.ErrorMessage = "Failed to update settings. Please try again."
		}
	}

	return renderCtx, settingsGeneralFormTemplate, nil
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	if user.SubscriptionID.Valid {
		subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve a subscription", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}

		if s.PlanService.IsSubscriptionActive(subscription.Status) && subscription.PaddleSubscriptionID.Valid {
			if err := s.PlanService.CancelSubscription(ctx, subscription.PaddleSubscriptionID.String); err != nil {
				slog.ErrorContext(ctx, "Failed to cancel Paddle subscription", "userID", user.ID, common.ErrAttr(err))
				s.RedirectError(http.StatusInternalServerError, w, r)
				return
			}
		}
	}

	if err := s.Store.SoftDeleteUser(ctx, user.ID); err == nil {
		s.logout(w, r)
	} else {
		slog.ErrorContext(ctx, "Failed to delete user", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}
}

func (s *Server) createAPIKeysSettingsModel(ctx context.Context, user *dbgen.User) *settingsAPIKeysRenderContext {
	commonCtx := s.CreateSettingsCommonRenderContext(common.APIKeysEndpoint, user)

	keys, err := s.Store.RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", common.ErrAttr(err))
		commonCtx.ErrorMessage = "Could not load API keys."
	}

	return &settingsAPIKeysRenderContext{
		SettingsCommonRenderContext: commonCtx,
		Keys:                        apiKeysToUserAPIKeys(keys, time.Now().UTC()),
	}
}

func (s *Server) getAPIKeysSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := s.createAPIKeysSettingsModel(ctx, user)

	return renderCtx, "", nil
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

func (s *Server) postAPIKeySettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", ErrInvalidRequestArg
	}

	renderCtx := s.createAPIKeysSettingsModel(ctx, user)

	formName := strings.TrimSpace(r.FormValue(common.ParamName))
	if len(formName) < 3 {
		renderCtx.NameError = "Name is too short."
		renderCtx.CreateOpen = true
		return renderCtx, settingsAPIKeysContentTemplate, nil
	}

	apiKeyRequestsPerSecond := 1.0
	if user.SubscriptionID.Valid {
		if subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32); err == nil {
			if plan, err := s.PlanService.FindPlanEx(subscription.PaddleProductID, subscription.PaddlePriceID, s.Stage,
				db.IsInternalSubscription(subscription.Source)); err == nil {
				apiKeyRequestsPerSecond = plan.APIRequestsPerSecond
			}
		}
	}

	months := monthsFromParam(ctx, r.FormValue(common.ParamMonths))
	tnow := time.Now().UTC()
	expiration := tnow.AddDate(0, months, 0)
	newKey, err := s.Store.CreateAPIKey(ctx, user.ID, formName, expiration, apiKeyRequestsPerSecond)
	if err == nil {
		userKey := apiKeyToUserAPIKey(newKey, tnow)
		userKey.Secret = db.UUIDToSecret(newKey.ExternalID)
		renderCtx.Keys = append(renderCtx.Keys, userKey)
		renderCtx.SuccessMessage = "API Key created successfully."
	} else {
		slog.ErrorContext(ctx, "Failed to create API key", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to create API key. Please try again."
	}

	return renderCtx, settingsAPIKeysContentTemplate, nil
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	keyID, value, err := common.IntPathArg(r, common.ParamKey)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse key path parameter", "value", value)
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if err := s.Store.DeleteAPIKey(ctx, user.ID, int32(keyID)); err != nil {
		slog.ErrorContext(ctx, "Failed to delete the API key", "keyID", keyID, common.ErrAttr(err))
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) getAccountStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	type point struct {
		Date  int64 `json:"x"`
		Value int   `json:"y"`
	}

	data := []*point{}

	timeFrom := time.Now().UTC().AddDate(-1 /*years*/, 0 /*months*/, 0 /*days*/)
	if stats, err := s.TimeSeries.ReadAccountStats(ctx, user.ID, timeFrom); err == nil {
		anyNonZero := false
		for _, st := range stats {
			if st.Count > 0 {
				anyNonZero = true
			}
			data = append(data, &point{Date: st.Timestamp.Unix(), Value: int(st.Count)})
		}

		// we want to show "No data available" on the client
		if !anyNonZero {
			data = []*point{}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve account stats", common.ErrAttr(err))
	}

	response := struct {
		Data []*point `json:"data"`
	}{
		Data: data,
	}

	common.SendJSONResponse(ctx, w, response, common.NoCacheHeaders)
}

func (s *Server) createUsageSettingsModel(ctx context.Context, user *dbgen.User) *settingsUsageRenderContext {
	renderCtx := &settingsUsageRenderContext{
		SettingsCommonRenderContext: s.CreateSettingsCommonRenderContext(common.UsageEndpoint, user),
	}

	if user.SubscriptionID.Valid {
		subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription for usage tab", common.ErrAttr(err))
			renderCtx.ErrorMessage = "Could not load subscription details for usage limits."
		} else {
			if plan, err := s.PlanService.FindPlanEx(subscription.PaddleProductID, subscription.PaddlePriceID, s.Stage,
				db.IsInternalSubscription(subscription.Source)); err == nil {
				renderCtx.Limit = int(plan.RequestsLimit)
			} else {
				slog.ErrorContext(ctx, "Failed to find billing plan for usage tab", "productID", subscription.PaddleProductID, "priceID", subscription.PaddlePriceID, common.ErrAttr(err))
				renderCtx.ErrorMessage = "Could not determine usage limits from your plan."
			}
		}
	} else {
		slog.DebugContext(ctx, "User does not have a subscription (usage tab)", "userID", user.ID)
		renderCtx.WarningMessage = "You don't have an active subscription."
	}

	return renderCtx
}

func (s *Server) getUsageSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := s.createUsageSettingsModel(ctx, user)

	return renderCtx, "", nil
}
