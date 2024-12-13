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
	settingsGeneralTemplate     = "settings/general.html"
	settingsTemplate            = "settings/settings.html"
	settingsGeneralFormTemplate = "settings/general-form.html"
	settingsAPIKeysTemplate     = "settings/apikeys.html"
	settingsBillingTemplate     = "settings/billing.html"
	settingsUsageTemplate       = "settings/usage.html"
)

var (
	errMissingSubsciption = errors.New("user does not have a subscription")
)

type settingsCommonRenderContext struct {
	Tab    int
	Email  string
	UserID int32
}

type settingsUsageRenderContext struct {
	alertRenderContext
	settingsCommonRenderContext
	Limit int
}

type settingsGeneralRenderContext struct {
	alertRenderContext
	settingsCommonRenderContext
	csrfRenderContext
	Name           string
	NameError      string
	EmailError     string
	TwoFactorError string
	TwoFactorEmail string
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
	settingsCommonRenderContext
	csrfRenderContext
	Name       string
	NameError  string
	Keys       []*userAPIKey
	CreateOpen bool
}

type settingsBillingRenderContext struct {
	alertRenderContext
	settingsCommonRenderContext
	csrfRenderContext
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

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	var renderCtx any
	var err error

	tabParam := r.URL.Query().Get(common.ParamTab)
	switch tabParam {
	case common.APIKeysEndpoint:
		renderCtx, _, err = s.getAPIKeysSettings(w, r)
	case common.BillingEndpoint:
		renderCtx, _, err = s.getBillingSettings(w, r)
	case common.UsageEndpoint:
		renderCtx, _, err = s.getUsageSettings(w, r)
	default:
		if (tabParam != common.GeneralEndpoint) && (tabParam != "") {
			slog.ErrorContext(ctx, "Unknown tab requested", "tab", tabParam)
		}
		renderCtx, _, err = s.getGeneralSettings(w, r)
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to create settings render context")
		return nil, "", err
	}

	return renderCtx, settingsTemplate, nil
}

func (s *Server) getGeneralSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := &settingsGeneralRenderContext{
		settingsCommonRenderContext: settingsCommonRenderContext{
			Tab:    0,
			Email:  user.Email,
			UserID: user.ID,
		},
		csrfRenderContext: s.createCsrfContext(user),
		Name:              user.Name,
	}

	return renderCtx, settingsGeneralTemplate, nil
}

func (s *Server) editEmail(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	sess := s.session(w, r)
	user, err := s.sessionUser(ctx, sess)
	if err != nil {
		return nil, "", err
	}

	code := twoFactorCode()

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		return nil, "", err
	}

	sess.Set(session.KeyTwoFactorCode, code)

	renderCtx := &settingsGeneralRenderContext{
		settingsCommonRenderContext: settingsCommonRenderContext{
			Tab:    0,
			Email:  user.Email,
			UserID: user.ID,
		},
		csrfRenderContext: s.createCsrfContext(user),
		Name:              user.Name,
		TwoFactorEmail:    common.MaskEmail(user.Email, '*'),
		EditEmail:         true,
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

	renderCtx := &settingsGeneralRenderContext{
		settingsCommonRenderContext: settingsCommonRenderContext{
			Tab:    0,
			Email:  user.Email,
			UserID: user.ID,
		},
		csrfRenderContext: s.createCsrfContext(user),
		Name:              user.Name,
		EditEmail:         (len(formEmail) > 0) && (formEmail != user.Email) && ((len(formName) == 0) || (formName == user.Name)),
	}

	anyChange := false
	sess := s.session(w, r)

	if renderCtx.EditEmail {
		renderCtx.Email = formEmail
		renderCtx.TwoFactorEmail = common.MaskEmail(user.Email, '*')

		if err := checkmail.ValidateFormat(formEmail); err != nil {
			slog.Warn("Failed to validate email format", common.ErrAttr(err))
			renderCtx.EmailError = "Email address is not valid."
			return renderCtx, settingsGeneralFormTemplate, nil
		}

		sentCode, hasSentCode := sess.Get(session.KeyTwoFactorCode).(int)
		formCode := r.FormValue(common.ParamVerificationCode)

		// we "used" the code now
		sess.Delete(session.KeyTwoFactorCode)

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
		if err := s.Store.UpdateUser(ctx, user.ID, renderCtx.Name, renderCtx.Email /*new email*/, user.Email /*old email*/); err == nil {
			renderCtx.SuccessMessage = "Settings were updated."
			renderCtx.EditEmail = false
			sess.Set(session.KeyUserName, renderCtx.Name)
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

		if billing.IsSubscriptionActive(subscription.Status) {
			if err := s.PaddleAPI.CancelSubscription(ctx, subscription.PaddleSubscriptionID); err != nil {
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

func (s *Server) getAPIKeysSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	keys, err := s.Store.RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", common.ErrAttr(err))
		return nil, "", err
	}

	renderCtx := &settingsAPIKeysRenderContext{
		settingsCommonRenderContext: settingsCommonRenderContext{
			Tab:    1,
			Email:  user.Email,
			UserID: user.ID,
		},
		csrfRenderContext: s.createCsrfContext(user),
		Keys:              apiKeysToUserAPIKeys(keys, time.Now().UTC()),
	}

	return renderCtx, settingsAPIKeysTemplate, nil
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

	keys, err := s.Store.RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", common.ErrAttr(err))
		return nil, "", err
	}

	renderCtx := &settingsAPIKeysRenderContext{
		settingsCommonRenderContext: settingsCommonRenderContext{
			Tab:    1,
			Email:  user.Email,
			UserID: user.ID,
		},
		csrfRenderContext: s.createCsrfContext(user),
		Keys:              apiKeysToUserAPIKeys(keys, time.Now().UTC()),
	}

	formName := strings.TrimSpace(r.FormValue(common.ParamName))
	if len(formName) < 3 {
		renderCtx.NameError = "Name is too short."
		renderCtx.CreateOpen = true
		return renderCtx, settingsAPIKeysTemplate, nil
	}

	apiKeyRequestsPerSecond := 1.0
	if user.SubscriptionID.Valid {
		if subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32); err == nil {
			if plan, err := billing.FindPlanByPriceAndProduct(subscription.PaddleProductID, subscription.PaddlePriceID, s.Stage); err == nil {
				apiKeyRequestsPerSecond = plan.APIRequestsPerSecond
			}
		}
	}

	months := monthsFromParam(ctx, r.FormValue(common.ParamMonths))
	tnow := time.Now().UTC()
	expiration := tnow.AddDate(0, months, 0)
	key, err := s.Store.CreateAPIKey(ctx, user.ID, formName, expiration, apiKeyRequestsPerSecond)
	if err == nil {
		userKey := apiKeyToUserAPIKey(key, tnow)
		userKey.Secret = db.UUIDToSecret(key.ExternalID)
		renderCtx.Keys = append(renderCtx.Keys, userKey)
	} else {
		slog.ErrorContext(ctx, "Failed to create API key", common.ErrAttr(err))
		// TODO: show error message
	}

	return renderCtx, settingsAPIKeysTemplate, nil
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

func (s *Server) createBillingRenderContext(ctx context.Context, user *dbgen.User) (*settingsBillingRenderContext, error) {
	renderCtx := &settingsBillingRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
		settingsCommonRenderContext: settingsCommonRenderContext{
			Tab:    2,
			Email:  user.Email,
			UserID: user.ID,
		},
	}

	if user.SubscriptionID.Valid {
		subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			return nil, err
		}

		renderCtx.IsSubscribed = billing.IsSubscriptionActive(subscription.Status)
		if renderCtx.IsSubscribed {
			if subscription.CancelFrom.Valid && subscription.CancelFrom.Time.After(time.Now()) {
				renderCtx.InfoMessage = fmt.Sprintf("Your subscription ends on %s.", subscription.CancelFrom.Time.Format("02 Jan 2006"))
			} else if subscription.TrialEndsAt.Valid && subscription.TrialEndsAt.Time.After(time.Now()) {
				renderCtx.InfoMessage = fmt.Sprintf("Your trial ends on %s.", subscription.TrialEndsAt.Time.Format("02 Jan 2006"))
			}

			if plan, err := billing.FindPlanByPriceAndProduct(subscription.PaddleProductID, subscription.PaddlePriceID, s.Stage); err == nil {
				renderCtx.CurrentPlan = plan
				renderCtx.YearlyBilling = plan.IsYearly(subscription.PaddlePriceID)
			} else {
				slog.ErrorContext(ctx, "Failed to find billing plan", "productID", subscription.PaddleProductID, "priceID", subscription.PaddlePriceID, common.ErrAttr(err))
			}
		}
	}

	if !renderCtx.IsSubscribed {
		renderCtx.WarningMessage = "You don't have an active subscription."
		renderCtx.CurrentPlan = &billing.Plan{}
	}

	if plans, ok := billing.GetPlansForStage(s.Stage); ok {
		renderCtx.Plans = plans
	}

	return renderCtx, nil
}

func (s *Server) getBillingSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx, err := s.createBillingRenderContext(ctx, user)
	if err != nil {
		return nil, "", err
	}

	if !renderCtx.IsSubscribed {
		w.Header().Set("HX-Trigger-After-Swap", "load-paddle")
	}

	return renderCtx, settingsBillingTemplate, nil
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
		return nil, errMissingSubsciption
	}

	subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user subscription", common.ErrAttr(err))
		http.Error(w, "", http.StatusInternalServerError)
		return nil, err
	}

	urls, err := s.PaddleAPI.GetManagementURLs(ctx, subscription.PaddleSubscriptionID)
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
		common.Redirect(urls.CancelURL, w, r)
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
		common.Redirect(urls.UpdateURL, w, r)
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
	plan, err := billing.FindPlanByProductID(product, s.Stage)
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

	renderCtx, err := s.createBillingRenderContext(ctx, user)
	if err != nil {
		return nil, "", err
	}

	if !renderCtx.IsSubscribed {
		slog.ErrorContext(ctx, "Attemp to preview subscription change while not subscribed", "userID", user.ID)
		return nil, "", errMissingSubsciption
	}

	subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user subscription", common.ErrAttr(err))
		return nil, "", err
	}

	if (priceID == subscription.PaddlePriceID) && (product == subscription.PaddleProductID) {
		slog.WarnContext(ctx, "No subscription changes required")
		return renderCtx, settingsBillingTemplate, nil
	}

	changePreview, err := s.PaddleAPI.PreviewChangeSubscription(ctx, subscription.PaddleSubscriptionID, priceID, 1 /*quantity*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to preview Paddle change", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to change your subscription. Please contact support for assistance."
		return renderCtx, settingsBillingTemplate, nil
	}

	renderCtx.PreviewOpen = true
	renderCtx.PreviewPlan = plan.Name
	renderCtx.PreviewPeriod = previewPeriod
	renderCtx.PreviewCharge = changePreview.ChargeAmount
	renderCtx.PreviewCurrency = changePreview.ChargeCurrency
	renderCtx.PreviewPriceID = priceID

	return renderCtx, settingsBillingTemplate, nil
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
	if _, err := billing.FindPlanByPriceID(priceID, s.Stage); err != nil {
		slog.ErrorContext(ctx, "PriceID is not valid", "priceID", priceID, common.ErrAttr(err))
		return nil, "", err
	}

	renderCtx, err := s.createBillingRenderContext(ctx, user)
	if err != nil {
		return nil, "", err
	}
	renderCtx.ClearAlerts()

	if !renderCtx.IsSubscribed {
		slog.ErrorContext(ctx, "Attemp to update subscription while not subscribed", "userID", user.ID)
		return nil, "", errMissingSubsciption
	}

	subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user subscription", common.ErrAttr(err))
		return nil, "", err
	}

	if priceID == subscription.PaddlePriceID {
		slog.WarnContext(ctx, "No subscription changes required")
		return renderCtx, settingsBillingTemplate, nil
	}

	if newPlan, err := billing.FindPlanByPriceID(priceID, s.Stage); (err == nil) && newPlan.IsDowngradeFor(renderCtx.CurrentPlan) {
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
				renderCtx.ErrorMessage = "To downgrade, last 2 months usage has to be within new plan's limits."
				return renderCtx, settingsBillingTemplate, nil
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to find new billing plan", "priceID", subscription.PaddlePriceID, common.ErrAttr(err))
	}

	if err := s.PaddleAPI.ChangeSubscription(ctx, subscription.PaddleSubscriptionID, priceID, 1 /*quantity*/); err == nil {
		renderCtx.SuccessMessage = "Subscription was updated successfully."
	} else {
		renderCtx.ErrorMessage = "Failed to update subscription. Please contact support for assistance."
	}

	return renderCtx, settingsBillingTemplate, nil
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

	common.SendJSONResponse(ctx, w, response, map[string]string{})
}

func (s *Server) getUsageSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := &settingsUsageRenderContext{
		settingsCommonRenderContext: settingsCommonRenderContext{
			Tab:    3,
			Email:  user.Email,
			UserID: user.ID,
		},
	}

	if user.SubscriptionID.Valid {
		subscription, err := s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", common.ErrAttr(err))
			return renderCtx, settingsUsageTemplate, nil
		}

		if plan, err := billing.FindPlanByPriceAndProduct(subscription.PaddleProductID, subscription.PaddlePriceID, s.Stage); err == nil {
			renderCtx.Limit = int(plan.RequestsLimit)
		} else {
			slog.ErrorContext(ctx, "Failed to find billing plan", "productID", subscription.PaddleProductID, "priceID", subscription.PaddlePriceID, common.ErrAttr(err))
		}
	}

	return renderCtx, settingsUsageTemplate, nil
}
