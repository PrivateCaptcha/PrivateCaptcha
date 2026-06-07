package portal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/api"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/rs/xid"
)

const (
	webhookPrefixPathLimit            = 12
	formDashboardTemplate             = "form/dashboard.html"
	formDashboardReportsTemplate      = "form/reports.html"
	formDashboardIntegrationsTemplate = "form/integrations.html"
	formDashboardSettingsTemplate     = "form/settings.html"
	formDashboardAuditLogsTemplate    = "form/auditlogs.html"
	formWizardTemplate                = "form-wizard/wizard.html"
	formWizardNewTemplate             = "form-wizard/new.html"
	formWizardSetupTemplate           = "form-wizard/client-setup.html"
	formTestTemplate                  = "form/settings-test-form.html"
	activeSubscriptionForFormError    = "You need an active subscription to create new forms."
	formReportsTabIndex               = 0
	formIntegrationsTabIndex          = 1
	formSettingsTabIndex              = 2
	formAuditLogsTabIndex             = 3
)

type formWizardRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	Name        string
	Domain      string
	URL         string
	NameError   string
	DomainError string
	URLError    string
	CurrentOrg  *UserOrg
	Step        int
}

type userForm struct {
	ID                string
	OrgID             string
	PropertyID        string
	Name              string
	URL               string
	Method            string
	WebhookPrefix     string
	ExternalID        string
	RetryRequestCount int
	RequestsPerMinute int
	Enabled           bool
	Active            bool
}

type orgFormsRenderContext struct {
	portalBaseRenderContext
	PaginationRenderContext
	Forms []*userForm
}

type formIntegrationRenderContext struct {
	CsrfRenderContext
	CurrentOrg *UserOrg
	Form       *userForm
	Sitekey    string
}

type formDashboardRenderContext struct {
	AlertRenderContext
	CsrfRenderContext
	Form      *userForm
	Org       *UserOrg
	NameError string
	Tab       int
	CanEdit   bool
}

type formDashboardIntegrationsRenderContext struct {
	formDashboardRenderContext
	Sitekey string
}

type formSettingsRenderContext struct {
	AlertRenderContext
	formDashboardRenderContext
	Orgs     []*UserOrg
	URLError string
	CanMove  bool
	TestBody string
}

type formAuditLogsRenderContext struct {
	formDashboardRenderContext
	AuditLogsRenderContext
	CanView bool
}

func (s *Server) validateFormsLimit(ctx context.Context, org *dbgen.Organization, sessUser *dbgen.User) string {
	owner, subscr, err := s.Store.Impl().RetrieveOrgOwnerWithSubscription(ctx, org, sessUser)
	if err != nil {
		return ""
	}

	isOrgOwner := org.UserID.Int32 == sessUser.ID

	ok, extra, err := s.SubscriptionLimits.CheckFormsLimit(ctx, org.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			if isOrgOwner {
				return activeSubscriptionForFormError
			}

			return "Organization owner needs an active subscription to create new forms."
		}
		return ""
	}

	if !ok {
		slog.WarnContext(ctx, "Forms limit check failed", "extra", extra, "userID", owner.ID, "subscriptionID", subscr.ID,
			"orgOwner", isOrgOwner, "internal", db.IsInternalSubscription(subscr.Source))

		if isOrgOwner {
			return "Forms limit reached for current subscription plan."
		}

		return "Forms limit reached for this organization's owner, contact them to upgrade."
	}

	return ""
}

func (s *Server) getNewOrgForm(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	data := &formWizardRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		CurrentOrg: &UserOrg{
			Name:  org.Name,
			ID:    s.IDHasher.Encrypt(int(org.ID)),
			Level: "",
		},
	}

	if isUserOrgOwner := org.UserID.Int32 == user.ID; isUserOrgOwner && !user.SubscriptionID.Valid {
		data.ErrorMessage = activeSubscriptionForFormError
	}

	return &ViewModel{Model: data, View: formWizardTemplate, IsNew: true}, nil
}

func (s *Server) postNewOrgForm(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	if err = r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, db.ErrInvalidInput
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	// Invited users cannot create properties - must join the org first
	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		slog.WarnContext(ctx, "User is only invited, not a member of this org", "orgID", org.ID, "userID", user.ID)
		return nil, db.ErrPermissions
	}

	renderCtx := &formWizardRenderContext{
		CsrfRenderContext:  s.CreateCsrfContext(user),
		AlertRenderContext: AlertRenderContext{},
		CurrentOrg:         orgToUserOrg(org, user.ID, s.IDHasher),
	}

	renderCtx.Name = strings.TrimSpace(r.FormValue(common.ParamName))
	if nameStatus := s.Store.Impl().ValidateFormName(ctx, renderCtx.Name, org); !nameStatus.Success() {
		renderCtx.NameError = nameStatus.String()
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate, IsNew: false}, nil
	}

	renderCtx.Domain = strings.TrimSpace(r.FormValue(common.ParamDomain))
	domain, err := common.ParseDomainName(renderCtx.Domain)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse domain name", "domain", renderCtx.Domain, common.ErrAttr(err))
		renderCtx.DomainError = common.StatusPropertyDomainFormatError.String()
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate, IsNew: false}, nil
	}

	_, ignoreError := r.Form[common.ParamIgnoreError]
	if domainStatus := s.validateDomainName(ctx, domain, ignoreError); !domainStatus.Success() {
		renderCtx.DomainError = domainStatus.String()
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate, IsNew: false}, nil
	}

	renderCtx.URL = strings.TrimSpace(r.FormValue(common.ParamURL))
	if len(renderCtx.URL) == 0 {
		renderCtx.URLError = "URL cannot be empty."
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate, IsNew: false}, nil
	}

	if err := s.FormURLVerifier.VerifyURL(ctx, renderCtx.URL); err != nil {
		slog.WarnContext(ctx, "Failed to verify form URL", "url", common.SafeURL(renderCtx.URL), common.ErrAttr(err))
		renderCtx.URLError = "URL is not valid."
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate, IsNew: false}, nil
	}

	if limitError := s.validatePropertiesLimit(ctx, org, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate, IsNew: false}, nil
	}

	if limitError := s.validateFormsLimit(ctx, org, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate, IsNew: false}, nil
	}

	propertyParams := db.NewDefaultPropertyParams("" /*name*/, domain, user.ID)
	form, property, auditEvents, err := s.Store.Impl().CreateNewForm(ctx, propertyParams, &dbgen.CreateFormParams{
		Name:              renderCtx.Name,
		URL:               renderCtx.URL,
		Fields:            []byte(`{}`),
		Enabled:           true,
		RequestsPerMinute: 10,
		RetryRequestCount: 0,
		Method:            dbgen.FormMethodPost,
	}, org)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create the form", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to create the form. Please try again later."
		return &ViewModel{Model: renderCtx, View: formWizardNewTemplate, IsNew: false}, nil
	}

	return &ViewModel{
		Model: &formIntegrationRenderContext{
			CsrfRenderContext: s.CreateCsrfContext(user),
			CurrentOrg:        orgToUserOrg(org, user.ID, s.IDHasher),
			Form:              formToUserForm(form, s.IDHasher),
			Sitekey:           db.UUIDToSiteKey(property.ExternalID),
		},
		View:        formWizardSetupTemplate,
		AuditEvents: auditEvents,
		IsNew:       true,
	}, nil
}

func webhookPrefixFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Hostname() == "") {
		return rawURL
	}

	prefix := u.Hostname()
	segment := strings.TrimPrefix(u.EscapedPath(), "/")
	if segment == "" {
		return prefix
	}

	if idx := strings.Index(segment, "/"); idx >= 0 {
		segment = segment[:idx]
	}

	if len(segment) > webhookPrefixPathLimit {
		segment = segment[:webhookPrefixPathLimit] + "..."
	}

	if segment == "" {
		return prefix
	}

	return prefix + "/" + segment
}

func formToUserForm(form *dbgen.Form, hasher common.IdentifierHasher) *userForm {
	if form == nil {
		return nil
	}

	requestsPerMinute := int(form.RequestsPerMinute)
	requestsPerMinute = max(1, min(requestsPerMinute, 60))

	return &userForm{
		ID:                hasher.Encrypt(int(form.ID)),
		OrgID:             hasher.Encrypt(int(form.OrgID.Int32)),
		PropertyID:        hasher.Encrypt(int(form.PropertyID)),
		Name:              form.Name,
		URL:               form.URL,
		Method:            formMethodToHTTPMethod(form.Method),
		WebhookPrefix:     webhookPrefixFromURL(form.URL),
		ExternalID:        db.UUIDToString(form.ExternalID),
		RetryRequestCount: int(form.RetryRequestCount),
		RequestsPerMinute: requestsPerMinute,
		Enabled:           form.Enabled,
		Active:            form.Active,
	}
}

func formMethodToHTTPMethod(method dbgen.FormMethod) string {
	switch method {
	case dbgen.FormMethodPut:
		return http.MethodPut
	case dbgen.FormMethodDelete:
		return http.MethodDelete
	case dbgen.FormMethodPatch:
		return http.MethodPatch
	case dbgen.FormMethodPost, "":
		return http.MethodPost
	default:
		return strings.ToUpper(string(method))
	}
}

func parseFormMethod(value string) (dbgen.FormMethod, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case http.MethodPost:
		return dbgen.FormMethodPost, nil
	case http.MethodPut:
		return dbgen.FormMethodPut, nil
	case http.MethodDelete:
		return dbgen.FormMethodDelete, nil
	case http.MethodPatch:
		return dbgen.FormMethodPatch, nil
	default:
		return "", db.ErrInvalidInput
	}
}

func formsToUserForms(ctx context.Context, forms []*dbgen.Form, hasher common.IdentifierHasher) []*userForm {
	result := make([]*userForm, 0, len(forms))

	for _, form := range forms {
		if form == nil {
			continue
		}

		if form.DeletedAt.Valid {
			slog.WarnContext(ctx, "Skipping soft-deleted form", "formID", form.ID, "orgID", form.OrgID, "deletedAt", form.DeletedAt)
			continue
		}

		result = append(result, formToUserForm(form, hasher))
	}

	return result
}

func periodFromPath(ctx context.Context, r *http.Request) common.TimePeriod {
	periodStr := r.PathValue(common.ParamPeriod)
	switch periodStr {
	case PeriodEndpointToday:
		return common.TimePeriodToday
	case PeriodEndpointWeek:
		return common.TimePeriodWeek
	case PeriodEndpointMonth:
		return common.TimePeriodMonth
	case PeriodEndpointYear:
		return common.TimePeriodYear
	default:
		slog.ErrorContext(ctx, "Incorrect period argument", "period", periodStr)
		return common.TimePeriodToday
	}
}

func parseRequestsPerMinute(ctx context.Context, value string) (int16, error) {
	i, err := strconv.ParseInt(value, 10, 16)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse requests per minute", "value", value, common.ErrAttr(err))
		return 0, db.ErrInvalidInput
	}

	const maxValue = 60
	const minValue = 1

	if (i < minValue) || (i > maxValue) {
		slog.ErrorContext(ctx, "Invalid value of requests per minute", "value", value)
		return 0, db.ErrInvalidInput
	}

	return int16(i), nil
}

func (s *Server) getFormStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		slog.WarnContext(ctx, "User is only invited, not a member of this org", "orgID", org.ID, "userID", user.ID)
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	form, err := s.Form(org, r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	period := periodFromPath(ctx, r)
	etag := common.GenerateETag(strconv.Itoa(int(user.ID)), strconv.Itoa(int(org.ID)), strconv.Itoa(int(form.ID)), period.String())
	if etagHeader := r.Header.Get(common.HeaderIfNoneMatch); len(etagHeader) > 0 && (etagHeader == etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	success := []*FormStatsPoint{}
	failure := []*FormStatsPoint{}

	if stats, err := s.TimeSeries.RetrieveFormStatsByPeriod(ctx, org.ID, form.ID, period); err == nil {
		anyNonZero := false
		for _, st := range stats {
			if (st.SuccessCount > 0) || (st.FailureCount > 0) {
				anyNonZero = true
			}
			success = append(success, &FormStatsPoint{Date: st.Timestamp.Unix(), Value: st.SuccessCount})
			failure = append(failure, &FormStatsPoint{Date: st.Timestamp.Unix(), Value: st.FailureCount})
		}

		if !anyNonZero {
			success = []*FormStatsPoint{}
			failure = []*FormStatsPoint{}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve form stats", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	cacheHeaders := map[string][]string{
		common.HeaderETag:         []string{etag},
		common.HeaderCacheControl: common.PrivateCacheControl1m,
	}

	common.SendJSONResponse(ctx, w, FormStatsResponse{Success: success, Failure: failure}, cacheHeaders)
}

func (s *Server) getOrgForm(w http.ResponseWriter, r *http.Request) (*formDashboardRenderContext, *dbgen.Form, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, nil, err
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		return nil, nil, err
	}

	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		slog.WarnContext(ctx, "User is only invited, not a member of this org", "orgID", org.ID, "userID", user.ID)
		return nil, nil, db.ErrPermissions
	}

	form, err := s.Form(org, r)
	if err != nil {
		return nil, nil, err
	}

	if !form.Enabled {
		slog.WarnContext(ctx, "Form is disabled", "formID", form.ID, "orgID", form.OrgID)
		return nil, nil, db.ErrDisabled
	}

	renderCtx := &formDashboardRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		Form:              formToUserForm(form, s.IDHasher),
		Org:               orgToUserOrg(org, user.ID, s.IDHasher),
		CanEdit:           (user.ID == org.UserID.Int32) || (user.ID == form.CreatorID.Int32),
	}

	return renderCtx, form, nil
}

func (s *Server) getFormDashboard(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	tabParam := r.URL.Query().Get(common.ParamTab)
	slog.Log(ctx, common.LevelTrace, "Form tab was requested", "tab", tabParam)

	var model Model
	switch tabParam {
	case common.IntegrationsEndpoint:
		renderCtx, err := s.getFormIntegrations(w, r)
		if err != nil {
			return nil, err
		}
		model = renderCtx
	case common.SettingsEndpoint:
		renderCtx, err := s.getOrgFormSettings(w, r)
		if err != nil {
			return nil, err
		}
		model = renderCtx
	case common.EventsEndpoint:
		renderCtx, _, err := s.getFormAuditLogs(w, r)
		if err != nil {
			return nil, err
		}
		model = renderCtx
	case "", common.ReportsEndpoint:
		renderCtx, _, err := s.getOrgForm(w, r)
		if err != nil {
			return nil, err
		}
		renderCtx.Tab = formReportsTabIndex
		model = renderCtx
	default:
		slog.ErrorContext(ctx, "Unknown form tab requested", "tab", tabParam)
		renderCtx, _, err := s.getOrgForm(w, r)
		if err != nil {
			return nil, err
		}
		renderCtx.Tab = formReportsTabIndex
		model = renderCtx
	}

	return &ViewModel{Model: model, View: formDashboardTemplate, IsNew: true}, nil
}

func (s *Server) getFormReportsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	renderCtx, _, err := s.getOrgForm(w, r)
	if err != nil {
		return nil, err
	}

	renderCtx.Tab = formReportsTabIndex

	return &ViewModel{Model: renderCtx, View: formDashboardReportsTemplate, IsNew: true}, nil
}

func (s *Server) getFormIntegrations(w http.ResponseWriter, r *http.Request) (*formDashboardIntegrationsRenderContext, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	dashboardCtx, form, err := s.getOrgForm(w, r)
	if err != nil {
		return nil, err
	}

	property, err := s.Store.Impl().RetrieveOrgProperty(ctx, org, form.PropertyID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve form property for integrations", "formID", form.ID, "propertyID", form.PropertyID, common.ErrAttr(err))
		return nil, err
	}

	renderCtx := &formDashboardIntegrationsRenderContext{
		formDashboardRenderContext: *dashboardCtx,
		Sitekey:                    db.UUIDToSiteKey(property.ExternalID),
	}
	renderCtx.Tab = formIntegrationsTabIndex

	return renderCtx, nil
}

func (s *Server) getFormIntegrationsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	renderCtx, err := s.getFormIntegrations(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: formDashboardIntegrationsTemplate, IsNew: true}, nil
}

func (s *Server) newFormAuditLogs(ctx context.Context, user *dbgen.User, logs []*dbgen.GetFormAuditLogsRow) []*UserAuditLog {
	result := make([]*UserAuditLog, 0, len(logs))

	for _, log := range logs {
		if ul, err := s.NewUserAuditLog(ctx, &log.AuditLog); err == nil {
			if log.Name.Valid && log.Email.Valid {
				ul.UserName = log.Name.String
				ul.UserEmail = common.MaskEmail(log.Email.String, '*')
			} else {
				ul.UserName = "Unknown User"
				ul.UserEmail = "-"
			}

			result = append(result, ul)
		}
	}

	return result
}

func (s *Server) getFormAuditLogsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	renderCtx, auditEvent, err := s.getFormAuditLogs(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: formDashboardAuditLogsTemplate, AuditEvents: singleAuditEvents(auditEvent), IsNew: true}, nil
}

func (s *Server) getOrgFormSettings(w http.ResponseWriter, r *http.Request) (*formSettingsRenderContext, error) {
	ctx := r.Context()
	dashboardCtx, form, err := s.getOrgForm(w, r)
	if err != nil {
		return nil, err
	}

	renderCtx := &formSettingsRenderContext{formDashboardRenderContext: *dashboardCtx, Orgs: []*UserOrg{}, CanMove: false}

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	if user.ID == form.CreatorID.Int32 {
		if orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user.ID); err == nil {
			renderCtx.Orgs = orgsToUserOrgs(orgs, s.IDHasher)
			for _, org := range orgs {
				if form.OrgID.Valid && (org.Organization.ID != form.OrgID.Int32) && (org.Level == dbgen.AccessLevelOwner) {
					renderCtx.CanMove = true
					break
				}
			}
		}
	}

	renderCtx.Tab = formSettingsTabIndex

	return renderCtx, nil
}

func (s *Server) getFormSettingsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	renderCtx, err := s.getOrgFormSettings(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: formDashboardSettingsTemplate, IsNew: true}, nil
}

func (s *Server) putForm(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	if err = r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}

	renderCtx, err := s.getOrgFormSettings(w, r)
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	form, err := s.Form(org, r)
	if err != nil {
		return nil, err
	}

	if !renderCtx.CanEdit {
		slog.WarnContext(ctx, "Insufficient permissions to edit form", "userID", user.ID, "orgUserID", org.UserID.Int32, "formUserID", form.CreatorID.Int32)
		renderCtx.ErrorMessage = common.StatusPropertyPermissionsError.String()
		return &ViewModel{Model: renderCtx, View: formDashboardSettingsTemplate, IsNew: false}, nil
	}

	name := strings.TrimSpace(r.FormValue(common.ParamName))
	renderCtx.Form.Name = name
	if name != form.Name {
		if nameStatus := s.Store.Impl().ValidateFormName(ctx, name, org); !nameStatus.Success() {
			renderCtx.NameError = nameStatus.String()
			renderCtx.Form.URL = strings.TrimSpace(r.FormValue(common.ParamURL))
			return &ViewModel{Model: renderCtx, View: formDashboardSettingsTemplate, IsNew: false}, nil
		}
	}

	urlValue := strings.TrimSpace(r.FormValue(common.ParamURL))
	if len(urlValue) == 0 {
		renderCtx.URLError = "URL cannot be empty."
		renderCtx.Form.URL = urlValue
		return &ViewModel{Model: renderCtx, View: formDashboardSettingsTemplate, IsNew: false}, nil
	}
	renderCtx.Form.URL = urlValue

	if err := s.FormURLVerifier.VerifyURL(ctx, urlValue); err != nil {
		slog.WarnContext(ctx, "Failed to verify form URL", "url", common.SafeURL(urlValue), common.ErrAttr(err))
		renderCtx.URLError = "URL is not valid."
		renderCtx.Form.URL = urlValue
		return &ViewModel{Model: renderCtx, View: formDashboardSettingsTemplate, IsNew: false}, nil
	}

	methodValue := strings.TrimSpace(r.FormValue(common.ParamMethod))
	method, err := parseFormMethod(methodValue)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse form method", "value", methodValue, common.ErrAttr(err))
		renderCtx.ErrorMessage = "Method is not valid."
		return &ViewModel{Model: renderCtx, View: formDashboardSettingsTemplate, IsNew: false}, nil
	}

	renderCtx.Form.Method = strings.ToUpper(methodValue)

	_, active := r.Form[common.ParamActive]
	var retryRequestCount int16
	if _, retry := r.Form[common.ParamRetryRequestCount]; retry {
		retryRequestCount = 1
	}
	rpmValue := r.FormValue(common.ParamRequestsPerMinute)
	requestsPerMinute, err := parseRequestsPerMinute(ctx, rpmValue)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse RPM", "value", rpmValue, common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to update settings."
		return &ViewModel{Model: renderCtx, View: formDashboardSettingsTemplate, IsNew: false}, nil
	}

	var auditEvent *common.AuditLogEvent
	if (name != form.Name) || (urlValue != form.URL) || (method != form.Method) || (active != form.Active) || (retryRequestCount != form.RetryRequestCount) || (requestsPerMinute != form.RequestsPerMinute) {
		params := &dbgen.UpdateFormParams{
			ID:                form.ID,
			Name:              name,
			URL:               urlValue,
			Active:            active,
			RetryRequestCount: retryRequestCount,
			RequestsPerMinute: requestsPerMinute,
			Method:            method,
		}

		var updatedForm *dbgen.Form
		if updatedForm, auditEvent, err = s.Store.Impl().UpdateForm(ctx, org, user, params); err != nil {
			renderCtx.ErrorMessage = "Failed to update settings. Please try again."
		} else {
			slog.InfoContext(ctx, "Edited form", "formID", form.ID, "orgID", org.ID)
			renderCtx.SuccessMessage = "Settings were updated"
			renderCtx.Form = formToUserForm(updatedForm, s.IDHasher)
		}
	}

	return &ViewModel{Model: renderCtx, View: formDashboardSettingsTemplate, AuditEvents: singleAuditEvents(auditEvent), IsNew: true}, nil
}

func (s *Server) submitFormDirectly(ctx context.Context, form *dbgen.Form, submission *api.FormSubmission) *api.FormSubmitResult {
	if err := s.FormURLVerifier.VerifyURL(ctx, form.URL); err != nil {
		return &api.FormSubmitResult{Success: false}
	}

	client := common.NewFormHTTPClient()

	formCopy := *form
	formCopy.RetryRequestCount = 0
	if formCopy.RedirectCount > 0 {
		client = common.NewFormRedirectHTTPClient(s.FormURLVerifier, formCopy.RedirectCount)
	}

	return api.SubmitFormWithRetry(ctx, client, &formCopy, submission)
}

func (s *Server) postTestForm(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	if err = r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	form, err := s.Form(org, r)
	if err != nil {
		return nil, err
	}

	renderCtx, err := s.getOrgFormSettings(w, r)
	if err != nil {
		return nil, err
	}

	if !renderCtx.CanEdit {
		slog.WarnContext(ctx, "Insufficient permissions to test form", "userID", user.ID, "formID", form.ID, "orgID", org.ID)
		renderCtx.ErrorMessage = common.StatusPropertyPermissionsError.String()
		return &ViewModel{Model: renderCtx, View: formTestTemplate, IsNew: false}, nil
	}

	body := r.FormValue(common.ParamBody)
	renderCtx.TestBody = body
	if err := s.FormURLVerifier.VerifyURL(ctx, form.URL); err != nil {
		slog.WarnContext(ctx, "Failed to verify test form URL", "url", common.SafeURL(form.URL), common.ErrAttr(err))
		renderCtx.ErrorMessage = "URL is not valid."
		return &ViewModel{Model: renderCtx, View: formTestTemplate, IsNew: false}, nil
	}

	values, err := url.ParseQuery(body)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse form body payload", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to parse body as url-encoded form."
		return &ViewModel{Model: renderCtx, View: formTestTemplate, IsNew: false}, nil
	}

	submission := &api.FormSubmission{
		ID:             xid.New().String(),
		FormExternalID: db.UUIDToString(form.ExternalID),
		Values:         values,
		UserAgent:      r.UserAgent(),
		Referer:        r.Header.Get(common.HeaderReferer),
		Time:           time.Now().UTC(),
	}

	result := s.submitFormDirectly(ctx, form, submission)
	if result.Success {
		renderCtx.SuccessMessage = fmt.Sprintf("Test succeeded. (HTTP %d)", result.StatusCode)
	} else if result.StatusCode > 0 {
		renderCtx.WarningMessage = fmt.Sprintf("Test failed. (HTTP %d)", result.StatusCode)
	} else {
		renderCtx.ErrorMessage = "Cannot submit form. Please try again later."
	}

	return &ViewModel{Model: renderCtx, View: formTestTemplate, IsNew: false}, nil
}

func (s *Server) deleteForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	form, err := s.Form(org, r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	property, err := s.Store.Impl().RetrieveOrgProperty(ctx, org, form.PropertyID)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	canDelete := (user.ID == org.UserID.Int32) || (user.ID == form.CreatorID.Int32)
	if !canDelete {
		slog.ErrorContext(ctx, "Not enough permissions to delete form", "userID", user.ID, "orgUserID", org.UserID.Int32, "formUserID", form.CreatorID.Int32)
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	auditEvents, err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		return impl.SoftDeleteForm(ctx, form, property, org, user)
	})
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)))+"?"+common.ParamTab+"="+common.FormsEndpoint, http.StatusOK, w, r)
	s.Store.AuditLog().RecordEvents(ctx, auditEvents, common.AuditLogSourcePortal)
}
