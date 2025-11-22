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
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
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
	apiKeyRowTemplate              = "settings-apikeys/key.html"

	// notifications
	apiKeyExpirationNotificationDays = 14
)

var (
	errNoTabs = errors.New("no settings tabs configured")
)

type SettingsTab struct {
	ID             string
	Name           string
	TemplatePrefix string
	ModelHandler   ViewModelHandler
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

func apiKeyToUserAPIKey(key *dbgen.APIKey, tnow time.Time, hasher common.IdentifierHasher) *userAPIKey {
	// in terms of "leaky bucket" logic
	capacity := float64(key.RequestsBurst)
	leakInterval := float64(time.Second) / key.RequestsPerSecond
	// {period} during which we can consume (or restore) {capacity}
	period := capacity * leakInterval
	periodsPerMinute := float64(time.Minute) / period
	requestsPerMinute := capacity * periodsPerMinute

	return &userAPIKey{
		ID:                hasher.Encrypt(int(key.ID)),
		Name:              key.Name,
		ExpiresAt:         key.ExpiresAt.Time.Format("02 Jan 2006"),
		ExpiresSoon:       key.ExpiresAt.Time.Sub(tnow) <= apiKeyExpirationNotificationDays*24*time.Hour,
		RequestsPerMinute: int(requestsPerMinute),
	}
}

func apiKeysToUserAPIKeys(keys []*dbgen.APIKey, tnow time.Time, hasher common.IdentifierHasher) []*userAPIKey {
	result := make([]*userAPIKey, 0, len(keys))

	for _, key := range keys {
		result = append(result, apiKeyToUserAPIKey(key, tnow, hasher))
	}

	return result
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	tabParam := r.URL.Query().Get(common.ParamTab)
	slog.Log(ctx, common.LevelTrace, "Settings tab was requested", "tab", tabParam)

	tab, err := s.findTab(ctx, tabParam)
	if err != nil {
		return nil, err
	}

	modelView, err := tab.ModelHandler(w, r)
	if err != nil {
		return nil, err
	}

	modelView.View = tab.TemplatePrefix + "page.html"

	return modelView, nil
}

func (s *Server) getSettingsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	tabID, err := common.StrPathArg(r, common.ParamTab)
	if err != nil {
		slog.ErrorContext(ctx, "Cannot retrieve tab from path", common.ErrAttr(err))
	}

	tab, err := s.findTab(ctx, tabID)
	if err != nil {
		return nil, err
	}

	modelView, err := tab.ModelHandler(w, r)
	if err != nil {
		return nil, err
	}

	modelView.View = tab.TemplatePrefix + "tab.html"

	return modelView, nil
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

func (s *Server) getGeneralSettings(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	renderCtx := s.createGeneralSettingsModel(ctx, user)

	return &ViewModel{
		Model:      renderCtx,
		View:       "",
		AuditEvent: newAccessAuditLogEvent(user, db.TableNameUsers, int64(user.ID), user.Name, common.SettingsEndpoint),
	}, nil
}

func (s *Server) editEmail(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	sess := s.Session(w, r)
	user, err := s.SessionUser(ctx, sess)
	if err != nil {
		return nil, err
	}

	renderCtx := s.createGeneralSettingsModel(ctx, user)
	renderCtx.EditEmail = true
	renderCtx.TwoFactorEmail = common.MaskEmail(user.Email, '*')

	code := twoFactorCode()
	location := r.Header.Get(s.CountryCodeHeader.Value())

	if err := s.Mailer.SendTwoFactor(ctx, user.Email, code, r.UserAgent(), location); err != nil {
		slog.ErrorContext(ctx, "Failed to send email message", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to send verification code. Please try again."
	} else {
		_ = sess.Set(session.KeyTwoFactorCode, code)
	}

	return &ViewModel{Model: renderCtx, View: settingsGeneralFormTemplate}, nil
}

func (s *Server) putGeneralSettings(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
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
			return &ViewModel{Model: renderCtx, View: settingsGeneralFormTemplate}, nil
		}

		sentCode, hasSentCode := sess.Get(ctx, session.KeyTwoFactorCode).(int)
		formCode := r.FormValue(common.ParamVerificationCode)

		// we "used" the code now
		_ = sess.Delete(session.KeyTwoFactorCode)

		if enteredCode, err := strconv.Atoi(formCode); !hasSentCode || (err != nil) || (enteredCode != sentCode) {
			slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "expected", sentCode, common.ErrAttr(err))
			renderCtx.TwoFactorError = "Code is not valid."
			return &ViewModel{Model: renderCtx, View: settingsGeneralFormTemplate}, nil
		}

		anyChange = (len(formEmail) > 0) && (formEmail != user.Email)
	} else /*edit name only*/ {
		renderCtx.Name = formName

		if formName != user.Name {
			if (len(formName) > 0) && (len(formName) < 3) {
				renderCtx.NameError = "Please use a longer name."
				return &ViewModel{Model: renderCtx, View: settingsGeneralFormTemplate}, nil
			}

			if !isUserNameValid(formName) {
				renderCtx.NameError = userNameErrorMessage
				return &ViewModel{Model: renderCtx, View: settingsGeneralFormTemplate}, nil
			}
		}

		anyChange = (len(formName) > 0) && (formName != user.Name)
	}

	var auditEvent *common.AuditLogEvent
	if anyChange {
		emailToUpdate := user.Email
		if renderCtx.EditEmail {
			emailToUpdate = formEmail
		}
		if auditEvent, err = s.Store.Impl().UpdateUser(ctx, user, renderCtx.Name, emailToUpdate, user.Email); err == nil {
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

	return &ViewModel{Model: renderCtx, View: settingsGeneralFormTemplate, AuditEvent: auditEvent}, nil
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	if user.SubscriptionID.Valid {
		subscription, err := s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve a subscription", common.ErrAttr(err))
			s.RedirectError(http.StatusInternalServerError, w, r)
			return
		}

		if s.PlanService.IsSubscriptionActive(subscription.Status) && subscription.ExternalSubscriptionID.Valid {
			if err := s.PlanService.CancelSubscription(ctx, subscription.ExternalSubscriptionID.String); err != nil {
				slog.ErrorContext(ctx, "Failed to cancel external subscription", "userID", user.ID, common.ErrAttr(err))
				s.RedirectError(http.StatusInternalServerError, w, r)
				return
			}
		}
	}

	if auditEvents, err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		auditEvent, err := impl.SoftDeleteUser(ctx, user)
		return []*common.AuditLogEvent{auditEvent}, err
	}); err == nil {
		job := s.Jobs.OffboardUser(user)
		go common.RunOneOffJob(common.CopyTraceID(ctx, context.Background()), job, job.NewParams())
		s.Store.AuditLog().RecordEvents(ctx, auditEvents)

		s.logout(w, r)
	} else {
		slog.ErrorContext(ctx, "Failed to delete user", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}
}

func (s *Server) createAPIKeysSettingsModel(ctx context.Context, user *dbgen.User) *settingsAPIKeysRenderContext {
	commonCtx := s.CreateSettingsCommonRenderContext(common.APIKeysEndpoint, user)

	keys, err := s.Store.Impl().RetrieveUserAPIKeys(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user API keys", common.ErrAttr(err))
		commonCtx.ErrorMessage = "Could not load API keys."
	}

	return &settingsAPIKeysRenderContext{
		SettingsCommonRenderContext: commonCtx,
		Keys:                        apiKeysToUserAPIKeys(keys, time.Now().UTC(), s.IDHasher),
	}
}

func (s *Server) getAPIKeysSettings(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	renderCtx := s.createAPIKeysSettingsModel(ctx, user)

	return &ViewModel{
		Model:      renderCtx,
		AuditEvent: newAccessAuditLogEvent(user, db.TableNameAPIKeys, int64(user.ID), user.Name, common.SettingsEndpoint),
	}, nil
}

func apiKeyDaysFromParam(ctx context.Context, param string) int {
	i, err := strconv.Atoi(param)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert days", "value", param, common.ErrAttr(err))
		return 30
	}

	switch i {
	case 1, 30, 90, 180, 365:
		return i
	default:
		return 30
	}
}

// NOTE: ReferenceID logic should stay the same forever for correct deduplication in DB
func apiKeyExpirationReference(id int32) string {
	return fmt.Sprintf("apikey/%v/expiration", id)
}

func createAPIKeyExpirationNotification(key *dbgen.APIKey, userKey *userAPIKey) *common.ScheduledNotification {
	prefixLen := 4 + len(db.APIKeyPrefix)

	return &common.ScheduledNotification{
		ReferenceID: apiKeyExpirationReference(key.ID),
		UserID:      key.UserID.Int32,
		Subject:     fmt.Sprintf("[%s] Your API key will expire soon", common.PrivateCaptcha),
		Data: &email.APIKeyExpirationContext{
			APIKeyContext: email.APIKeyContext{
				APIKeyName:         key.Name,
				APIKeyPrefix:       userKey.Secret[0:min(prefixLen, len(userKey.Secret))],
				APIKeySettingsPath: fmt.Sprintf("%s?%s=%s", common.SettingsEndpoint, common.ParamTab, common.APIKeysEndpoint),
			},
			ExpireDays: apiKeyExpirationNotificationDays,
		},
		DateTime:     key.ExpiresAt.Time.AddDate(0, 0, -apiKeyExpirationNotificationDays),
		TemplateHash: email.APIKeyExirationTemplate.Hash(),
		Persistent:   false,
		Condition:    common.NotificationWithSubscription,
	}
}

// NOTE: ReferenceID logic should stay the same forever for correct deduplication in DB
func apiKeyExpiredReference(id int32) string {
	return fmt.Sprintf("apikey/%v/expired", id)
}

func createAPIKeyExpiredNotification(key *dbgen.APIKey, userKey *userAPIKey) *common.ScheduledNotification {
	prefixLen := 4 + len(db.APIKeyPrefix)

	return &common.ScheduledNotification{
		ReferenceID: apiKeyExpiredReference(key.ID),
		UserID:      key.UserID.Int32,
		Subject:     fmt.Sprintf("[%s] Your API key has expired", common.PrivateCaptcha),
		Data: email.APIKeyContext{
			APIKeyName:         key.Name,
			APIKeyPrefix:       userKey.Secret[0:min(prefixLen, len(userKey.Secret))],
			APIKeySettingsPath: fmt.Sprintf("%s?%s=%s", common.SettingsEndpoint, common.ParamTab, common.APIKeysEndpoint),
		},
		DateTime:     key.ExpiresAt.Time,
		TemplateHash: email.APIKeyExpiredTemplate.Hash(),
		Persistent:   false,
		Condition:    common.NotificationWithSubscription,
	}
}

func checkAPIKeyNameValid(ctx context.Context, name string) bool {
	const allowedPunctuation = "-_.()[]"

	for i, r := range name {
		switch {
		case unicode.IsLetter(r):
			continue
		case unicode.IsDigit(r):
			continue
		case unicode.IsSpace(r):
			continue
		case strings.ContainsRune(allowedPunctuation, r):
			continue
		default:
			slog.WarnContext(ctx, "Name contains invalid characters", "position", i, "rune", r)
			return false
		}
	}

	return true
}

func (s *Server) postAPIKeySettings(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}

	renderCtx := s.createAPIKeysSettingsModel(ctx, user)

	formName := strings.TrimSpace(r.FormValue(common.ParamName))
	if len(formName) < 3 {
		renderCtx.NameError = "Name is too short."
		renderCtx.CreateOpen = true
		return &ViewModel{Model: renderCtx, View: settingsAPIKeysContentTemplate}, nil
	}

	if !checkAPIKeyNameValid(ctx, formName) {
		renderCtx.NameError = "Name contains invalid characters."
		renderCtx.CreateOpen = true
		return &ViewModel{Model: renderCtx, View: settingsAPIKeysContentTemplate}, nil
	}

	if _, err := s.Store.Impl().FindUserAPIKeyByName(ctx, user, formName); err == nil {
		renderCtx.NameError = "API key with such name already exists."
		renderCtx.CreateOpen = true
		return &ViewModel{Model: renderCtx, View: settingsAPIKeysContentTemplate}, nil
	}

	apiKeyRequestsPerSecond := 1.0
	if user.SubscriptionID.Valid {
		if subscription, err := s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32); err == nil {
			if plan, err := s.PlanService.FindPlan(subscription.ExternalProductID, subscription.ExternalPriceID, s.Stage,
				db.IsInternalSubscription(subscription.Source)); err == nil {
				apiKeyRequestsPerSecond = plan.APIRequestsPerSecond()
			}
		}
	}

	days := apiKeyDaysFromParam(ctx, r.FormValue(common.ParamDays))
	tnow := time.Now().UTC()
	newKey, auditEvent, err := s.Store.Impl().CreateAPIKey(ctx, user, formName, tnow, time.Duration(days)*24*time.Hour, apiKeyRequestsPerSecond)
	if err == nil {
		userKey := apiKeyToUserAPIKey(newKey, tnow, s.IDHasher)
		userKey.Secret = db.UUIDToSecret(newKey.ExternalID)
		renderCtx.Keys = append(renderCtx.Keys, userKey)
		renderCtx.SuccessMessage = "API Key created successfully."

		go common.RunAdHocFunc(common.CopyTraceID(ctx, context.Background()), func(bctx context.Context) error {
			return s.createAPIKeyExpiryNotifications(bctx, newKey, userKey)
		})
	} else {
		slog.ErrorContext(ctx, "Failed to create API key", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to create API key. Please try again."
	}

	return &ViewModel{Model: renderCtx, View: settingsAPIKeysContentTemplate, AuditEvent: auditEvent}, nil
}

func (s *Server) createAPIKeyExpiryNotifications(ctx context.Context, key *dbgen.APIKey, userKey *userAPIKey) error {
	var anyError error
	minNotificationDate := time.Now().UTC().AddDate(0, 0, apiKeyExpirationNotificationDays)
	if key.ExpiresAt.Valid && key.ExpiresAt.Time.After(minNotificationDate) {
		if _, err := s.Store.Impl().CreateUserNotification(ctx, createAPIKeyExpirationNotification(key, userKey)); err != nil {
			anyError = err
		}
	}

	if _, err := s.Store.Impl().CreateUserNotification(ctx, createAPIKeyExpiredNotification(key, userKey)); err != nil {
		anyError = err
	}

	return anyError
}

func (s *Server) rotateAPIKey(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	keyID, value, err := common.IntPathArg(r, common.ParamKey, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse key path parameter", "value", value)
		return nil, errInvalidPathArg
	}

	key, auditEvent, err := s.Store.Impl().RotateAPIKey(ctx, user, keyID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to rotate the API key", "keyID", keyID, common.ErrAttr(err))
		return nil, err
	}

	userKey := apiKeyToUserAPIKey(key, time.Now().UTC(), s.IDHasher)
	userKey.Secret = db.UUIDToSecret(key.ExternalID)

	go common.RunAdHocFunc(common.CopyTraceID(ctx, context.Background()), func(bctx context.Context) error {
		var anyError error
		if err := s.deleteAPIKeyExpiryNotifications(bctx, user, keyID); err != nil {
			anyError = err
		}
		if err := s.createAPIKeyExpiryNotifications(bctx, key, userKey); err != nil {
			anyError = err
		}

		return anyError
	})

	return &ViewModel{Model: userKey, View: apiKeyRowTemplate, AuditEvent: auditEvent}, nil
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	keyID, value, err := common.IntPathArg(r, common.ParamKey, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse key path parameter", "value", value, common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if auditEvent, err := s.Store.Impl().DeleteAPIKey(ctx, user, keyID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete the API key", "keyID", keyID, common.ErrAttr(err))
		http.Error(w, "", http.StatusInternalServerError)
		return
	} else {
		s.Store.AuditLog().RecordEvent(ctx, auditEvent)
	}

	go common.RunAdHocFunc(common.CopyTraceID(ctx, context.Background()), func(bctx context.Context) error {
		return s.deleteAPIKeyExpiryNotifications(bctx, user, keyID)
	})

	w.WriteHeader(http.StatusOK)
}

func (s *Server) deleteAPIKeyExpiryNotifications(ctx context.Context, user *dbgen.User, keyID int32) error {
	var anyError error
	if err := s.Store.Impl().DeletePendingUserNotification(ctx, user, apiKeyExpirationReference(keyID)); err != nil {
		anyError = err
	}
	if err := s.Store.Impl().DeletePendingUserNotification(ctx, user, apiKeyExpiredReference(keyID)); err != nil {
		anyError = err
	}
	return anyError
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
	if stats, err := s.TimeSeries.RetrieveAccountStats(ctx, user.ID, timeFrom); err == nil {
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
		subscription, err := s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription for usage tab", common.ErrAttr(err))
			renderCtx.ErrorMessage = "Could not load subscription details for usage limits."
		} else {
			if plan, err := s.PlanService.FindPlan(subscription.ExternalProductID, subscription.ExternalPriceID, s.Stage,
				db.IsInternalSubscription(subscription.Source)); err == nil {
				renderCtx.Limit = int(plan.RequestsLimit())
			} else {
				slog.ErrorContext(ctx, "Failed to find billing plan for usage tab", "productID", subscription.ExternalProductID, "priceID", subscription.ExternalPriceID, common.ErrAttr(err))
				renderCtx.ErrorMessage = "Could not determine usage limits from your plan."
			}
		}
	} else {
		slog.DebugContext(ctx, "User does not have a subscription", "tab", "usage", "userID", user.ID)
		renderCtx.WarningMessage = "You don't have an active subscription."
	}

	return renderCtx
}

func (s *Server) getUsageSettings(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	renderCtx := s.createUsageSettingsModel(ctx, user)

	return &ViewModel{Model: renderCtx}, nil
}
