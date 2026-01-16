package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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

	// API key read flags
	apiKeyReadWriteSuffix = "_read_write"
	apiKeyReadOnlySuffix  = "_read_only"

	apiKeyScopePuzzle = "captcha"
	apiKeyScopePortal = "portal"

	unknownOrgNameFormat = "Unknown Organization #%d"
)

var (
	errNoTabs             = errors.New("no settings tabs configured")
	errInvalidAPIKeyScope = errors.New("invalid API key scope")
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
	PropertiesCount         int
	OrgsCount               int
	IncludedPropertiesCount int
	IncludedOrgsCount       int
	Limit                   int64
}

type accountStatsPoint struct {
	Date   int64 `json:"x"`
	Value  int   `json:"y"`
	Series int   `json:"s"`
}

type accountStatsRawPoint struct {
	OrgID int32
	Date  int64
	Value int
}

type accountStatsSeries struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

type accountStatsResponse struct {
	Series []*accountStatsSeries `json:"series"`
	Data   []*accountStatsPoint  `json:"data"`
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
	Scope             string
	RequestsPerMinute int
	OrgName           string
	ExpiresSoon       bool
	ReadOnly          bool
}

type settingsAPIKeysRenderContext struct {
	SettingsCommonRenderContext
	Name       string
	NameError  string
	OrgError   string
	Keys       []*userAPIKey
	Orgs       []*userOrg
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

	var scope string
	switch key.Scope {
	case dbgen.ApiKeyScopePortal:
		scope = apiKeyScopePortal
	case dbgen.ApiKeyScopePuzzle:
		scope = apiKeyScopePuzzle
	}

	return &userAPIKey{
		ID:                hasher.Encrypt(int(key.ID)),
		Name:              key.Name,
		ExpiresAt:         key.ExpiresAt.Time.Format("02 Jan 2006"),
		ExpiresSoon:       key.ExpiresAt.Time.Sub(tnow) <= apiKeyExpirationNotificationDays*24*time.Hour,
		RequestsPerMinute: int(requestsPerMinute),
		Scope:             scope,
		ReadOnly:          key.Readonly,
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
		// NOTE: we don't quit intentionally as this is a "soft" error
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

	code := twoFactorCode(ctx)
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
	tnow := time.Now().UTC()
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
		codeTimestamp, ok := sess.Get(ctx, session.KeyTwoFactorCodeTimestamp).(time.Time)
		if !ok {
			slog.ErrorContext(ctx, "Failed to get verification code timestamp")
		}
		formCode := r.FormValue(common.ParamVerificationCode)

		// we "used" the code now
		_ = sess.Delete(session.KeyTwoFactorCode)
		_ = sess.Delete(session.KeyTwoFactorCodeTimestamp)

		if enteredCode, err := strconv.Atoi(formCode); !hasSentCode || (err != nil) || (enteredCode != sentCode) || (!codeTimestamp.IsZero() && tnow.After(codeTimestamp.Add(twoFactorCodeDuration))) {
			slog.WarnContext(ctx, "Code verification failed", "actual", formCode, "expected", sentCode, "timestamp", codeTimestamp, common.ErrAttr(err))
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
		s.Store.AuditLog().RecordEvents(ctx, auditEvents, common.AuditLogSourcePortal)

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

	renderCtx := &settingsAPIKeysRenderContext{
		SettingsCommonRenderContext: commonCtx,
		Keys:                        apiKeysToUserAPIKeys(keys, time.Now().UTC(), s.IDHasher),
	}

	if orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user.ID); err == nil {
		renderCtx.Orgs = orgsToUserOrgs(orgs, s.IDHasher)

		orgNameMap := make(map[int32]string, len(orgs))
		for _, org := range orgs {
			orgNameMap[org.Organization.ID] = org.Organization.Name
		}
		for i, key := range keys {
			if key.OrgID.Valid {
				if name, ok := orgNameMap[key.OrgID.Int32]; ok {
					renderCtx.Keys[i].OrgName = name
				} else {
					// if this shows up in monitoring, rewrite RetrieveUserAPIKeys() to JOIN with Orgs
					slog.ErrorContext(ctx, "Organization name not found for API key", "keyID", key.ID, "orgID", key.OrgID.Int32)
				}
			}
		}
	}

	return renderCtx
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
		TemplateHash: email.APIKeyExpirationTemplate.Hash(),
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

func parseAPIKeyScope(scope string) (dbgen.ApiKeyScope, bool, error) {
	switch scope {
	case apiKeyScopePortal + apiKeyReadWriteSuffix:
		return dbgen.ApiKeyScopePortal, false, nil
	case apiKeyScopePortal + apiKeyReadOnlySuffix:
		return dbgen.ApiKeyScopePortal, true, nil
	case apiKeyScopePuzzle:
		return dbgen.ApiKeyScopePuzzle, false, nil
	default:
		return dbgen.ApiKeyScopePuzzle, false, errInvalidAPIKeyScope
	}
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

	scopeStr := strings.TrimSpace(r.FormValue(common.ParamScope))
	scope, readOnly, err := parseAPIKeyScope(scopeStr)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse API Key scope", "scope", scopeStr, common.ErrAttr(err))
		renderCtx.WarningMessage = "Failed to create API key with invalid scope."
		return &ViewModel{Model: renderCtx, View: settingsAPIKeysContentTemplate}, nil
	}

	apiKeyRequestsPerSecond := 1.0
	var minAPIKeyRequestsBurst int32 = 5
	if user.SubscriptionID.Valid {
		minAPIKeyRequestsBurst = 20
		if subscription, err := s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32); err == nil {
			if plan, err := s.PlanService.FindPlan(subscription.ExternalProductID, subscription.ExternalPriceID, s.Stage,
				db.IsInternalSubscription(subscription.Source)); err == nil {
				if scope == dbgen.ApiKeyScopePuzzle {
					apiKeyRequestsPerSecond = plan.APIRequestsPerSecond()
				} else {
					apiKeyRequestsPerSecond = max(1.0, math.Ceil(math.Log(plan.APIRequestsPerSecond())))
				}
			}
		}
	}

	pgOrgID := db.InvalidInt
	var orgName string
	orgIDStr := strings.TrimSpace(r.FormValue(common.ParamOrg))
	if (orgIDStr != common.All) && (len(orgIDStr) > 0) {
		var orgID int
		var oerr error
		if orgID, oerr = s.IDHasher.Decrypt(orgIDStr); oerr == nil {
			var org *dbgen.Organization
			if org, _, oerr = s.Store.Impl().RetrieveUserOrganization(ctx, user, int32(orgID)); oerr == nil {
				pgOrgID = db.Int(org.ID)
				orgName = org.Name
			}
		}
		if oerr != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user org", "orgID", orgIDStr, common.ErrAttr(oerr))
			renderCtx.OrgError = "Selected organization does not exist."
			renderCtx.CreateOpen = true
			return &ViewModel{Model: renderCtx, View: settingsAPIKeysContentTemplate}, nil
		}
	}

	// current logic is that initial values will be set per plan and adjusted manually in DB if requested by customer
	burst := max(minAPIKeyRequestsBurst, int32(apiKeyRequestsPerSecond*5))
	days := apiKeyDaysFromParam(ctx, r.FormValue(common.ParamDays))
	tnow := time.Now().UTC()
	period := time.Duration(days) * 24 * time.Hour
	params := &dbgen.CreateAPIKeyParams{
		Name:              formName,
		ExpiresAt:         db.Timestampz(tnow.Add(period)),
		RequestsPerSecond: apiKeyRequestsPerSecond,
		RequestsBurst:     burst,
		Period:            period,
		Scope:             scope,
		Readonly:          readOnly,
		OrgID:             pgOrgID,
	}
	newKey, auditEvent, err := s.Store.Impl().CreateAPIKey(ctx, user, params)
	if err == nil {
		userKey := apiKeyToUserAPIKey(newKey, tnow, s.IDHasher)
		userKey.OrgName = orgName
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
	var errs []error
	minNotificationDate := time.Now().UTC().AddDate(0, 0, apiKeyExpirationNotificationDays)
	if key.ExpiresAt.Valid && key.ExpiresAt.Time.After(minNotificationDate) {
		if _, err := s.Store.Impl().CreateUserNotification(ctx, createAPIKeyExpirationNotification(key, userKey)); err != nil {
			errs = append(errs, err)
		}
	}

	if _, err := s.Store.Impl().CreateUserNotification(ctx, createAPIKeyExpiredNotification(key, userKey)); err != nil {
		errs = append(errs, err)
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
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

	if key.OrgID.Valid {
		if org, _, err := s.Store.Impl().RetrieveUserOrganization(ctx, user, key.OrgID.Int32); err == nil {
			userKey.OrgName = org.Name
		}
	}

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
		s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)
	}

	go common.RunAdHocFunc(common.CopyTraceID(ctx, context.Background()), func(bctx context.Context) error {
		return s.deleteAPIKeyExpiryNotifications(bctx, user, keyID)
	})

	w.WriteHeader(http.StatusOK)
}

func (s *Server) deleteAPIKeyExpiryNotifications(ctx context.Context, user *dbgen.User, keyID int32) error {
	var errs []error
	if err := s.Store.Impl().DeletePendingUserNotification(ctx, user, apiKeyExpirationReference(keyID)); err != nil {
		errs = append(errs, err)
	}
	if err := s.Store.Impl().DeletePendingUserNotification(ctx, user, apiKeyExpiredReference(keyID)); err != nil {
		errs = append(errs, err)
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}

func (s *Server) getAccountStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	timeFrom := time.Now().UTC().AddDate(-1 /*years*/, 0 /*months*/, 0 /*days*/).Truncate(24 * time.Hour)

	response, err := s.buildAccountStatsResponse(ctx, user.ID, timeFrom)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve account stats", common.ErrAttr(err))
	}

	if response == nil {
		response = &accountStatsResponse{
			Series: []*accountStatsSeries{},
			Data:   []*accountStatsPoint{},
		}
	}

	common.SendJSONResponse(ctx, w, response, common.NoCacheHeaders)
}

func (s *Server) buildAccountStatsResponse(ctx context.Context, userID int32, timeFrom time.Time) (*accountStatsResponse, error) {
	dataRaw := []*accountStatsRawPoint{}
	orgIDs := make(map[int32]struct{})
	orgNames := make(map[int32]string)
	seriesIndex := make(map[int32]int)
	seriesList := []*accountStatsSeries{}

	stats, err := s.TimeSeries.RetrieveAccountStats(ctx, userID, timeFrom)
	if err != nil {
		return nil, err
	}

	anyNonZero := false
	for _, st := range stats {
		if st.Count > 0 {
			anyNonZero = true
		}
		dataRaw = append(dataRaw, &accountStatsRawPoint{OrgID: st.OrgID, Date: st.Timestamp.Unix(), Value: int(st.Count)})
		orgIDs[st.OrgID] = struct{}{}
	}

	if orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, userID); err == nil {
		for _, org := range orgs {
			orgID := org.Organization.ID
			orgName := org.Organization.Name
			orgNames[orgID] = orgName
			if _, ok := orgIDs[orgID]; ok {
				index := len(seriesList)
				seriesIndex[orgID] = index
				seriesList = append(seriesList, &accountStatsSeries{Name: orgName, Index: index})
			}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve user organizations for account stats; will use placeholder organization names", common.ErrAttr(err))
	}

	unknownCount := 0
	for orgID := range orgIDs {
		if _, ok := seriesIndex[orgID]; ok {
			continue
		}
		unknownCount++
		name := orgNames[orgID]
		if name == "" {
			name = fmt.Sprintf(unknownOrgNameFormat, unknownCount)
		}
		index := len(seriesList)
		seriesIndex[orgID] = index
		seriesList = append(seriesList, &accountStatsSeries{Name: name, Index: index})
	}

	data := make([]*accountStatsPoint, 0, len(dataRaw))
	for _, pt := range dataRaw {
		if index, ok := seriesIndex[pt.OrgID]; ok {
			data = append(data, &accountStatsPoint{Date: pt.Date, Value: pt.Value, Series: index})
		} else {
			slog.WarnContext(ctx, "Account stats series not found for point", "orgID", pt.OrgID, "date", pt.Date)
		}
	}

	// we want to show "No data available" on the client
	if !anyNonZero {
		data = []*accountStatsPoint{}
		seriesList = []*accountStatsSeries{}
	}

	return &accountStatsResponse{
		Series: seriesList,
		Data:   data,
	}, nil
}

func (s *Server) createUsageSettingsModel(ctx context.Context, user *dbgen.User) *settingsUsageRenderContext {
	renderCtx := &settingsUsageRenderContext{
		SettingsCommonRenderContext: s.CreateSettingsCommonRenderContext(common.UsageEndpoint, user),
	}

	if orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user.ID); err == nil {
		count := 0
		for _, org := range orgs {
			if org.Level == dbgen.AccessLevelOwner {
				count++
			}
		}
		renderCtx.OrgsCount = count
	}

	if count, err := s.Store.Impl().RetrieveUserPropertiesCount(ctx, user.ID); err == nil {
		renderCtx.PropertiesCount = int(count)
	}

	if user.SubscriptionID.Valid {
		subscription, err := s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription for usage tab", common.ErrAttr(err))
			renderCtx.ErrorMessage = "Could not load subscription details for usage limits."
		} else {
			renderCtx.Limit, _ = s.SubscriptionLimits.RequestsLimit(ctx, subscription)
			renderCtx.IncludedPropertiesCount, _ = s.SubscriptionLimits.PropertiesLimit(ctx, subscription)
			renderCtx.IncludedOrgsCount, _ = s.SubscriptionLimits.OrgsLimit(ctx, subscription)
		}
	} else {
		slog.DebugContext(ctx, "User does not have a subscription", "tab", "usage", "userID", user.ID)
		renderCtx.WarningMessage = "You don't have an active subscription."
	}

	if (renderCtx.Limit == 0) ||
		(renderCtx.IncludedOrgsCount == 0) ||
		(renderCtx.IncludedPropertiesCount == 0) {
		if len(renderCtx.WarningMessage) == 0 {
			renderCtx.WarningMessage = "Could not determine usage limits from your plan."
		}
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
