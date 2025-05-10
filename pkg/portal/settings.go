package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
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
	settingsBillingTemplatePrefix = "settings-billing/"
	settingsUsageTemplatePrefix   = "settings-usage/"

	// Other templates
	settingsGeneralFormTemplate    = "settings-general/form.html"
	settingsAPIKeysContentTemplate = "settings-apikeys/content.html"
	settingsBillingContentTemplate = "settings-billing/content.html"
	internalSubscriptionMessage    = "Please contact support to change internal subscription."
)

var (
	errMissingSubscription  = errors.New("user does not have a subscription")
	errInternalSubscription = errors.New("internal subscription")
	errInvalidSubscription  = errors.New("invalid subscription")
	errNoTabs               = errors.New("no settings tabs configured")
)

type SettingsTab struct {
	ID             string
	Name           string
	TemplatePrefix string
	ModelHandler   ModelFunc
}

// settingsTabViewModel is used for rendering the navigation in templates
type settingsTabViewModel struct {
	ID           string
	Name         string
	IconTemplate string
	IsActive     bool
}

type settingsCommonRenderContext struct {
	alertRenderContext
	csrfRenderContext

	// For navigation and content rendering
	Tabs        []*settingsTabViewModel
	ActiveTabID string
	Email       string
	UserID      int32

	// NOTE: these 2 are here because scripts.html is common for all settings endpoints
	// otherwise their place is in settingsBillingRenderContext
	PaddleEnvironment string
	PaddleClientToken string
}

type settingsUsageRenderContext struct {
	settingsCommonRenderContext
	Limit int
}

type settingsGeneralRenderContext struct {
	settingsCommonRenderContext
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
	settingsCommonRenderContext
	Name       string
	NameError  string
	Keys       []*userAPIKey
	CreateOpen bool
}

type settingsBillingRenderContext struct {
	settingsCommonRenderContext
	Plans           []*billing.Plan
	CurrentPlan     *billing.Plan
	PreviewPlan     string
	PreviewPeriod   string
	PreviewCharge   int
	PreviewCurrency string
	PreviewPriceID  string
	YearlyBilling   bool
	IsSubscribed    bool
	PreviewOpen     bool
	CanManage       bool
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

func createTabViewModels(activeTabID string, tabs []*SettingsTab) []*settingsTabViewModel {
	viewModels := make([]*settingsTabViewModel, 0, len(tabs))
	for _, tab := range tabs {
		viewModels = append(viewModels, &settingsTabViewModel{
			ID:           tab.ID,
			Name:         tab.Name,
			IsActive:     tab.ID == activeTabID,
			IconTemplate: tab.TemplatePrefix + "icon.html",
		})
	}
	return viewModels
}

func (s *Server) createSettingsCommonRenderContext(activeTabID string, user *dbgen.User) settingsCommonRenderContext {
	viewModels := createTabViewModels(activeTabID, s.SettingsTabs)

	return settingsCommonRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
		ActiveTabID:       activeTabID,
		Tabs:              viewModels,
		Email:             user.Email,
		UserID:            user.ID,
		PaddleEnvironment: s.PaddleAPI.Environment(),
		PaddleClientToken: s.PaddleAPI.ClientToken(),
	}
}

func (s *Server) createGeneralSettingsModel(ctx context.Context, user *dbgen.User) *settingsGeneralRenderContext {
	return &settingsGeneralRenderContext{
		settingsCommonRenderContext: s.createSettingsCommonRenderContext(common.GeneralEndpoint, user),
		Name:                        user.Name,
	}
}

func (s *Server) getGeneralSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := s.createGeneralSettingsModel(ctx, user)

	return renderCtx, "", nil
}

func (s *Server) editEmail(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	sess := s.session(w, r)
	user, err := s.sessionUser(ctx, sess)
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

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", errInvalidRequestArg
	}

	formName := strings.TrimSpace(r.FormValue(common.ParamName))
	formEmail := strings.TrimSpace(r.FormValue(common.ParamEmail))

	renderCtx := s.createGeneralSettingsModel(ctx, user)
	renderCtx.EditEmail = (len(formEmail) > 0) && (formEmail != user.Email) && ((len(formName) == 0) || (formName == user.Name))

	anyChange := false
	sess := s.session(w, r)

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
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	if user.SubscriptionID.Valid {
		subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve a subscription", common.ErrAttr(err))
			s.redirectError(http.StatusInternalServerError, w, r)
			return
		}

		if s.PlanService.IsSubscriptionActive(subscription.Status) && subscription.PaddleSubscriptionID.Valid {
			if err := s.PaddleAPI.CancelSubscription(ctx, subscription.PaddleSubscriptionID.String); err != nil {
				slog.ErrorContext(ctx, "Failed to cancel Paddle subscription", "userID", user.ID, common.ErrAttr(err))
				s.redirectError(http.StatusInternalServerError, w, r)
				return
			}
		}
	}

	if err := s.Store.SoftDeleteUser(ctx, user.ID); err == nil {
		s.logout(w, r)
	} else {
		slog.ErrorContext(ctx, "Failed to delete user", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}
}

func (s *Server) createAPIKeysSettingsModel(ctx context.Context, user *dbgen.User) *settingsAPIKeysRenderContext {
	commonCtx := s.createSettingsCommonRenderContext(common.APIKeysEndpoint, user)

	keys, err := s.Store.RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", common.ErrAttr(err))
		commonCtx.ErrorMessage = "Could not load API keys."
	}

	return &settingsAPIKeysRenderContext{
		settingsCommonRenderContext: commonCtx,
		Keys:                        apiKeysToUserAPIKeys(keys, time.Now().UTC()),
	}
}

func (s *Server) getAPIKeysSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
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
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", errInvalidRequestArg
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
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	keyID, value, err := common.IntPathArg(r, common.ParamKey)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse key path parameter", "value", value)
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	if err := s.Store.DeleteAPIKey(ctx, user.ID, int32(keyID)); err != nil {
		slog.ErrorContext(ctx, "Failed to delete the API key", "keyID", keyID, common.ErrAttr(err))
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) createBillingSettingsModel(ctx context.Context, user *dbgen.User) *settingsBillingRenderContext {
	renderCtx := &settingsBillingRenderContext{
		settingsCommonRenderContext: s.createSettingsCommonRenderContext(common.BillingEndpoint, user),
		CurrentPlan:                 &billing.Plan{},
	}

	if plans, ok := s.PlanService.GetPlansForStage(s.Stage); ok {
		renderCtx.Plans = plans
	}

	isSubscribed := false
	var subscription *dbgen.Subscription

	if user.SubscriptionID.Valid {
		var err error
		subscription, err = s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve subscription for billing tab", "userID", user.ID, common.ErrAttr(err))
		}
	}

	if subscription != nil {
		isInternalSubscription := db.IsInternalSubscription(subscription.Source)
		isInternalTrial := isInternalSubscription && s.PlanService.IsSubscriptionTrialing(subscription.Status)
		renderCtx.IsSubscribed = !isInternalTrial && s.PlanService.IsSubscriptionActive(subscription.Status)
		if renderCtx.IsSubscribed {
			renderCtx.CanManage = !isInternalSubscription

			if subscription.CancelFrom.Valid && subscription.CancelFrom.Time.After(time.Now()) {
				renderCtx.InfoMessage = fmt.Sprintf("Your subscription ends on %s.", subscription.CancelFrom.Time.Format("02 Jan 2006"))
			} else if subscription.TrialEndsAt.Valid && subscription.TrialEndsAt.Time.After(time.Now()) {
				renderCtx.InfoMessage = fmt.Sprintf("Your trial ends on %s.", subscription.TrialEndsAt.Time.Format("02 Jan 2006"))
			}

			if plan, err := s.PlanService.FindPlanEx(subscription.PaddleProductID, subscription.PaddlePriceID, s.Stage, isInternalSubscription); err == nil {
				renderCtx.CurrentPlan = plan
				renderCtx.YearlyBilling = plan.IsYearly(subscription.PaddlePriceID)
				isSubscribed = true
			} else {
				slog.ErrorContext(ctx, "Failed to find billing plan", "productID", subscription.PaddleProductID, "priceID", subscription.PaddlePriceID, common.ErrAttr(err))
				renderCtx.ErrorMessage = "Could not determine your current plan details."
			}
		} else if isInternalTrial && subscription.TrialEndsAt.Valid && subscription.TrialEndsAt.Time.After(time.Now()) {
			renderCtx.InfoMessage = fmt.Sprintf("Your free evaluation ends on %s.", subscription.TrialEndsAt.Time.Format("02 Jan 2006"))
			isSubscribed = true
		}
	} else {
		renderCtx.ErrorMessage = "Could not load your subscription details."
	}

	if !isSubscribed && (renderCtx.ErrorMessage == "") {
		renderCtx.WarningMessage = "You don't have an active subscription."
	}

	return renderCtx
}

func (s *Server) getBillingSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := s.createBillingSettingsModel(ctx, user)

	if !renderCtx.IsSubscribed {
		w.Header().Set("HX-Trigger-After-Swap", "load-paddle")
	}

	return renderCtx, "", nil
}

func (s *Server) retrieveUserManagementURLs(w http.ResponseWriter, r *http.Request) (*billing.ManagementURLs, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return nil, err
	}

	if !user.SubscriptionID.Valid {
		slog.ErrorContext(ctx, "Cannot get Paddle URLs without subscription", "userID", user.ID)
		http.Error(w, "", http.StatusInternalServerError)
		return nil, errMissingSubscription
	}

	subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user subscription", common.ErrAttr(err))
		http.Error(w, "", http.StatusInternalServerError)
		return nil, err
	}

	if db.IsInternalSubscription(subscription.Source) {
		slog.WarnContext(ctx, "Cannot modify internal subscription", "userID", user.ID, "subscription", subscription.Source)
		http.Error(w, "", http.StatusNotAcceptable)
		return nil, errInternalSubscription
	}

	if !subscription.PaddleSubscriptionID.Valid {
		slog.ErrorContext(ctx, "Paddle Subscription ID is NULL", "userID", user.ID, "subscriptionID", subscription.ID)
		http.Error(w, "", http.StatusInternalServerError)
		return nil, errInvalidSubscription
	}

	urls, err := s.PaddleAPI.GetManagementURLs(ctx, subscription.PaddleSubscriptionID.String)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to fetch Paddle URLs", common.ErrAttr(err))
		http.Error(w, "", http.StatusInternalServerError)
		return nil, err
	}

	return urls, nil
}

func (s *Server) getCancelSubscription(w http.ResponseWriter, r *http.Request) {
	urls, err := s.retrieveUserManagementURLs(w, r)
	if err != nil {
		return
	}

	if len(urls.CancelURL) > 0 {
		common.Redirect(urls.CancelURL, http.StatusOK, w, r)
	} else {
		http.Error(w, "URL is empty", http.StatusInternalServerError)
	}
}

func (s *Server) getUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	urls, err := s.retrieveUserManagementURLs(w, r)
	if err != nil {
		return
	}

	if len(urls.UpdateURL) > 0 {
		common.Redirect(urls.UpdateURL, http.StatusOK, w, r)
	} else {
		http.Error(w, "URL is empty", http.StatusInternalServerError)
	}
}

func (s *Server) postBillingPreview(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", err
	}
	product := r.FormValue(common.ParamProduct)
	plan, err := s.PlanService.FindPlanByProductID(product, s.Stage)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find plan by product ID", "productID", product, common.ErrAttr(err))
		return nil, "", err
	}

	previewPeriod := "monthly"
	priceID := plan.PaddlePriceIDMonthly
	if yearly := common.ParseBoolean(r.FormValue(common.ParamYearly)); yearly || (len(plan.PaddlePriceIDMonthly) == 0) {
		priceID = plan.PaddlePriceIDYearly
		previewPeriod = "annual"
	}

	renderCtx := s.createBillingSettingsModel(ctx, user)

	if !renderCtx.IsSubscribed {
		renderCtx.ErrorMessage = "You must have an active subscription to change plans."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		renderCtx.ErrorMessage = "Could not retrieve your subscription details."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	if (priceID == subscription.PaddlePriceID) && (product == subscription.PaddleProductID) {
		renderCtx.InfoMessage = "You are already on this plan."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	if db.IsInternalSubscription(subscription.Source) {
		renderCtx.ErrorMessage = internalSubscriptionMessage
		return renderCtx, settingsBillingContentTemplate, nil
	}

	if !subscription.PaddleSubscriptionID.Valid {
		renderCtx.WarningMessage = "Invalid subscription. Please contact support for assistance."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	changePreview, err := s.PaddleAPI.PreviewChangeSubscription(ctx, subscription.PaddleSubscriptionID.String, priceID, 1 /*quantity*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to preview Paddle change", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to change your subscription. Please contact support for assistance."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	renderCtx.PreviewOpen = true
	renderCtx.PreviewPlan = plan.Name
	renderCtx.PreviewPeriod = previewPeriod
	renderCtx.PreviewCharge = changePreview.ChargeAmount
	renderCtx.PreviewCurrency = changePreview.ChargeCurrency
	renderCtx.PreviewPriceID = priceID

	return renderCtx, settingsBillingContentTemplate, nil
}

func (s *Server) putBilling(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", err
	}

	priceID := r.FormValue(common.ParamPrice)
	newPlan, err := s.PlanService.FindPlanByPriceID(priceID, s.Stage)
	if err != nil {
		slog.ErrorContext(ctx, "PriceID is not valid", "priceID", priceID, common.ErrAttr(err))
		return nil, "", err
	}

	renderCtx := s.createBillingSettingsModel(ctx, user)
	renderCtx.ClearAlerts()

	if !renderCtx.IsSubscribed {
		slog.ErrorContext(ctx, "Attempt to update subscription while not subscribed", "userID", user.ID)
		renderCtx.ErrorMessage = "You must have an active subscription to change plans."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user subscription", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Could not retrieve your subscription details."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	if priceID == subscription.PaddlePriceID {
		slog.WarnContext(ctx, "No subscription changes required")
		renderCtx.InfoMessage = "You are already on this plan."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	if newPlan.IsDowngradeFor(renderCtx.CurrentPlan) {
		slog.DebugContext(ctx, "Downgrade attempt detected", "oldPlan", renderCtx.CurrentPlan.Name, "newPlan", newPlan.Name)
		timeFrom := time.Now().UTC().AddDate(0 /*years*/, -3 /*months*/, 0 /*days*/)
		if stats, err := s.TimeSeries.ReadAccountStats(ctx, user.ID, timeFrom); err == nil {
			anyHigher := false

			for _, stat := range stats {
				if !newPlan.IsLegitUsage(int64(stat.Count)) {
					slog.WarnContext(ctx, "Found exceeding usage", "timestamp", stat.Timestamp, "count", stat.Count, "limit", newPlan.RequestsLimit)
					anyHigher = true
					break
				}
			}

			if anyHigher {
				renderCtx.ErrorMessage = "To downgrade, your usage in the last 2 months must be within the new plan's limits."
				return renderCtx, settingsBillingContentTemplate, nil
			}
		} else {
			renderCtx.ErrorMessage = "Could not verify usage for downgrade. Please try again or contact support."
			return renderCtx, settingsBillingContentTemplate, nil
		}
	} else {
		slog.ErrorContext(ctx, "Failed to find new billing plan", "priceID", subscription.PaddlePriceID, common.ErrAttr(err))
	}

	if db.IsInternalSubscription(subscription.Source) {
		renderCtx.ErrorMessage = internalSubscriptionMessage
		return renderCtx, settingsBillingContentTemplate, nil
	}

	if !subscription.PaddleSubscriptionID.Valid {
		renderCtx.WarningMessage = "Invalid subscription. Please contact support for assistance."
		return renderCtx, settingsBillingContentTemplate, nil
	}

	if err := s.PaddleAPI.ChangeSubscription(ctx, subscription.PaddleSubscriptionID.String, priceID, 1 /*quantity*/); err == nil {
		renderCtx.SuccessMessage = "Subscription was updated successfully."
	} else {
		slog.ErrorContext(ctx, "Failed to update Paddle subscription", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to update subscription. Please contact support for assistance."
	}

	return renderCtx, settingsBillingContentTemplate, nil
}

func (s *Server) getAccountStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.sessionUser(ctx, s.session(w, r))
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
		settingsCommonRenderContext: s.createSettingsCommonRenderContext(common.UsageEndpoint, user),
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

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := s.createUsageSettingsModel(ctx, user)

	return renderCtx, "", nil
}
