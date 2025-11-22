package portal

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"golang.org/x/net/idna"
)

const (
	createPropertyFormTemplate            = "property-wizard/form.html"
	createOrgFormTemplate                 = "org-wizard/form.html"
	propertyDashboardTemplate             = "property/dashboard.html"
	propertyDashboardReportsTemplate      = "property/reports.html"
	propertyDashboardSettingsTemplate     = "property/settings.html"
	propertyDashboardIntegrationsTemplate = "property/integrations.html"
	propertyDashboardAuditLogsTemplate    = "property/auditlogs.html"
	propertyWizardTemplate                = "property-wizard/wizard.html"
	maxPropertyNameLength                 = 255
	propertySettingsPropertyID            = "371d58d2-f8b9-44e2-ac2e-e61253274bae"
	propertySettingsTabIndex              = 2
	propertyIntegrationsTabIndex          = 1
	propertyAuditLogsTabIndex             = 3
	activeSubscriptionForPropertyError    = "You need an active subscription to create new properties."
)

var validityDurations = []time.Duration{
	5 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
	2 * 24 * time.Hour,
	7 * 24 * time.Hour,
}

type difficultyLevelsRenderContext struct {
	EasyLevel   int
	NormalLevel int
	HardLevel   int
}

type propertyWizardRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	Name        string
	Domain      string
	NameError   string
	DomainError string
	CurrentOrg  *userOrg
}

type userProperty struct {
	ID               string
	OrgID            string
	Name             string
	Domain           string
	Sitekey          string
	Level            int
	Growth           int
	ValidityInterval int
	MaxReplayCount   int
	AllowSubdomains  bool
	AllowLocalhost   bool
	AllowReplay      bool
}

type orgPropertiesRenderContext struct {
	CsrfRenderContext
	Properties []*userProperty
	CurrentOrg *userOrg
}

type propertyDashboardRenderContext struct {
	AlertRenderContext
	CsrfRenderContext
	// scripts.html is shared so captcha context has to be shared too
	CaptchaRenderContext
	Property  *userProperty
	Org       *userOrg
	NameError string
	Tab       int
	CanEdit   bool
}

type propertySettingsRenderContext struct {
	propertyDashboardRenderContext
	difficultyLevelsRenderContext
	Orgs     []*userOrg
	MinLevel int
	MaxLevel int
	CanMove  bool
}

func (pc *propertySettingsRenderContext) UpdateLevels() {
	const epsilon = common.DifficultyDelta

	pc.MinLevel = max(1, pc.EasyLevel-epsilon)
	pc.MaxLevel = min(int(common.MaxDifficultyLevel), pc.HardLevel+epsilon)

	pc.Property.Level = max(pc.MinLevel, min(pc.MaxLevel, pc.Property.Level))
}

type propertyIntegrationsRenderContext struct {
	propertyDashboardRenderContext
	Sitekey string
}

type propertyAuditLogsRenderContext struct {
	propertyDashboardRenderContext
	AuditLogsRenderContext
	AlertRenderContext
	CanView bool
}

func createDifficultyLevelsRenderContext() difficultyLevelsRenderContext {
	return difficultyLevelsRenderContext{
		EasyLevel:   int(common.DifficultyLevelSmall),
		NormalLevel: int(common.DifficultyLevelMedium),
		HardLevel:   int(common.DifficultyLevelHigh),
	}
}

func propertyToUserProperty(p *dbgen.Property, hasher common.IdentifierHasher) *userProperty {
	up := &userProperty{
		ID:               hasher.Encrypt(int(p.ID)),
		OrgID:            hasher.Encrypt(int(p.OrgID.Int32)),
		Name:             p.Name,
		Domain:           p.Domain,
		Level:            int(p.Level.Int16),
		Growth:           growthLevelToIndex(p.Growth),
		Sitekey:          db.UUIDToSiteKey(p.ExternalID),
		ValidityInterval: validityIntervalToIndex(p.ValidityInterval),
		AllowReplay:      (p.MaxReplayCount > 1),
		MaxReplayCount:   max(1, int(p.MaxReplayCount)),
		AllowSubdomains:  p.AllowSubdomains,
		AllowLocalhost:   p.AllowLocalhost,
	}

	return up
}

func propertiesToUserProperties(ctx context.Context, properties []*dbgen.Property, hasher common.IdentifierHasher) []*userProperty {
	result := make([]*userProperty, 0, len(properties))

	for _, p := range properties {
		if p.DeletedAt.Valid {
			slog.WarnContext(ctx, "Skipping soft-deleted property", "propID", p.ID, "orgID", p.OrgID, "deleteAt", p.DeletedAt)
			continue
		}

		result = append(result, propertyToUserProperty(p, hasher))
	}

	return result
}

func growthLevelToIndex(level dbgen.DifficultyGrowth) int {
	switch level {
	case dbgen.DifficultyGrowthConstant:
		return 0
	case dbgen.DifficultyGrowthSlow:
		return 1
	case dbgen.DifficultyGrowthMedium:
		return 2
	case dbgen.DifficultyGrowthFast:
		return 3
	default:
		return 2
	}
}

func growthLevelFromIndex(ctx context.Context, index string) dbgen.DifficultyGrowth {
	i, err := strconv.Atoi(index)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert growth level", "value", index, common.ErrAttr(err))
		return dbgen.DifficultyGrowthMedium
	}

	switch i {
	case 0:
		return dbgen.DifficultyGrowthConstant
	case 1:
		return dbgen.DifficultyGrowthSlow
	case 2:
		return dbgen.DifficultyGrowthMedium
	case 3:
		return dbgen.DifficultyGrowthFast
	default:
		slog.WarnContext(ctx, "Invalid growth level index", "index", i)
		return dbgen.DifficultyGrowthMedium
	}
}

func validityIntervalToIndex(period time.Duration) int {
	for i, d := range validityDurations {
		if d == period {
			return i
		}
	}

	return 3
}

func validityIntervalFromIndex(ctx context.Context, index string) time.Duration {
	i, err := strconv.Atoi(index)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert validity period", "value", index, common.ErrAttr(err))
		return puzzle.DefaultValidityPeriod
	}

	if i >= 0 && i < len(validityDurations) {
		return validityDurations[i]
	}

	slog.WarnContext(ctx, "Invalid validity period index", "index", i)
	return puzzle.DefaultValidityPeriod
}

func parseMaxReplayCount(ctx context.Context, value string) int32 {
	i, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse max replay count", "value", value, common.ErrAttr(err))
		return 1
	}

	const maxValue = 1_000_000
	const minValue = 1

	if (i < minValue) || (i > maxValue) {
		slog.ErrorContext(ctx, "Invalid value of max replay count", "value", value)
	}

	return max(minValue, min(int32(i), maxValue))
}

func difficultyLevelFromValue(ctx context.Context, value string, minLevel, maxLevel int) common.DifficultyLevel {
	i, err := strconv.Atoi(value)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert difficulty level", "value", value, common.ErrAttr(err))
		return common.DifficultyLevelMedium
	}

	if (i <= 0) || (i > int(common.MaxDifficultyLevel)) {
		return common.DifficultyLevelMedium
	}

	return common.DifficultyLevel(max(minLevel, min(maxLevel, i)))
}

func (s *Server) getNewOrgProperty(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	data := &propertyWizardRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		CurrentOrg: &userOrg{
			Name:  org.Name,
			ID:    s.IDHasher.Encrypt(int(org.ID)),
			Level: "",
		},
	}

	// this is a quick check, longer check is done in POST
	if isUserOrgOwner := org.UserID.Int32 == user.ID; isUserOrgOwner && !user.SubscriptionID.Valid {
		data.ErrorMessage = activeSubscriptionForPropertyError
	}

	return &ViewModel{Model: data, View: propertyWizardTemplate}, nil
}

func (s *Server) validatePropertyName(ctx context.Context, name string, org *dbgen.Organization) string {
	if (len(name) == 0) || (len(name) > maxPropertyNameLength) {
		slog.WarnContext(ctx, "Name length is invalid", "length", len(name))

		if len(name) == 0 {
			return "Name cannot be empty."
		} else {
			return "Name is too long."
		}
	}

	const allowedPunctuation = "'-_.:()[]"

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
			return "Property name contains invalid characters."
		}
	}

	if _, err := s.Store.Impl().FindOrgProperty(ctx, name, org); err != db.ErrRecordNotFound {
		slog.WarnContext(ctx, "Property already exists", "name", name, common.ErrAttr(err))
		return "Property with this name already exists."
	}

	return ""
}

func (s *Server) validateDomainName(ctx context.Context, domain string, ignoreResolveError bool) string {
	if len(domain) == 0 {
		return "Domain name cannot be empty."
	}

	const localhostError = "Localhost is not allowed as a domain."

	if common.IsLocalhost(domain) {
		return localhostError
	}

	if common.IsIPAddress(domain) {
		return "IP address cannot be used as a domain."
	}

	_, err := idna.Lookup.ToASCII(domain)
	if err != nil {
		slog.WarnContext(ctx, "Failed to validate domain name", "domain", domain, common.ErrAttr(err))
		return "Domain name is not valid."
	}

	if ignoreResolveError {
		slog.WarnContext(ctx, "Skipping resolving domain name", "domain", domain)
		return ""
	}

	const timeout = 3 * time.Second
	rctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	var r net.Resolver
	names, err := r.LookupIPAddr(rctx, domain)
	if err == nil && len(names) > 0 {
		anyNonLocal := false
		for _, n := range names {
			if !n.IP.IsLoopback() {
				anyNonLocal = true
				break
			}
		}

		if !anyNonLocal {
			slog.WarnContext(ctx, "Only loopback IPs are resolved", "domain", domain, "first", names[0])
			return localhostError
		}

		slog.DebugContext(ctx, "Resolved domain name", "domain", domain, "ips", len(names), "first", names[0])
		return ""
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to resolve domain name", "domain", domain, common.ErrAttr(err))
	}

	return "Failed to resolve domain name."
}

func (s *Server) validatePropertiesLimit(ctx context.Context, org *dbgen.Organization, sessUser *dbgen.User) string {
	var subscr *dbgen.Subscription
	var err error

	isUserOrgOwner := org.UserID.Int32 == sessUser.ID
	userIDToCheck := sessUser.ID

	if isUserOrgOwner {
		if sessUser.SubscriptionID.Valid {
			subscr, err = s.Store.Impl().RetrieveSubscription(ctx, sessUser.SubscriptionID.Int32)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to retrieve session user subscription", "userID", sessUser.ID, common.ErrAttr(err))
				return ""
			}
		}
	} else {
		slog.DebugContext(ctx, "Session user is not org owner", "userID", sessUser.ID, "orgUserID", org.UserID.Int32)

		orgUser, err := s.Store.Impl().RetrieveUser(ctx, org.UserID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve org's owner user by ID", "id", org.UserID.Int32, common.ErrAttr(err))
			return ""
		}

		userIDToCheck = orgUser.ID
		subscr = nil

		if orgUser.SubscriptionID.Valid {
			subscr, err = s.Store.Impl().RetrieveSubscription(ctx, orgUser.SubscriptionID.Int32)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to retrieve org owner's subscription", "userID", org.UserID.Int32, common.ErrAttr(err))
				return ""
			}
		}
	}

	return s.doValidatePropertiesLimit(ctx, subscr, userIDToCheck, isUserOrgOwner)
}

func (s *Server) doValidatePropertiesLimit(ctx context.Context, subscr *dbgen.Subscription, userID int32, isOrgOwner bool) string {
	if (subscr == nil) || !s.PlanService.IsSubscriptionActive(subscr.Status) {
		if isOrgOwner {
			return activeSubscriptionForPropertyError
		}

		return "Organization owner needs an active subscription to create new properties."
	}

	isInternalSubscription := db.IsInternalSubscription(subscr.Source)
	plan, err := s.PlanService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, s.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return ""
	}

	count, err := s.Store.Impl().RetrieveUserPropertiesCount(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve properties count", "userID", userID, common.ErrAttr(err))
		return ""
	}

	if !plan.CheckPropertiesLimit(int(count)) {
		slog.WarnContext(ctx, "Properties limit check failed", "properties", count, "userID", userID, "subscriptionID", subscr.ID,
			"plan", plan.Name(), "internal", isInternalSubscription)

		if isOrgOwner {
			return "Properties limit reached on your current plan, please upgrade to create more."
		}

		return "Properties limit reached for this organization's owner, contact them to upgrade."
	}

	return ""
}

func (s *Server) echoPuzzle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var level common.DifficultyLevel
	if difficultyParam, err := common.StrPathArg(r, common.ParamDifficulty); err == nil {
		level = difficultyLevelFromValue(ctx, difficultyParam, 1, int(common.MaxDifficultyLevel))
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve difficulty argument", common.ErrAttr(err))
		level = common.DifficultyLevelSmall
	}

	sitekey := r.URL.Query().Get(common.ParamSiteKey)
	uuid := db.UUIDFromSiteKey(sitekey)

	p := puzzle.NewComputePuzzle(0 /*puzzle ID*/, uuid.Bytes, uint8(level))
	if err := p.Init(puzzle.DefaultValidityPeriod); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	_ = s.PuzzleEngine.Write(ctx, p, nil /*extra salt*/, w)
}

// This one cannot be "MVC" function because it redirects in case of success
func (s *Server) postNewOrgProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	org, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &propertyWizardRenderContext{
		CsrfRenderContext:  s.CreateCsrfContext(user),
		AlertRenderContext: AlertRenderContext{},
		CurrentOrg:         orgToUserOrg(org, user.ID, s.IDHasher),
	}

	renderCtx.Name = strings.TrimSpace(r.FormValue(common.ParamName))
	if nameError := s.validatePropertyName(ctx, renderCtx.Name, org); len(nameError) > 0 {
		renderCtx.NameError = nameError
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	renderCtx.Domain = strings.TrimSpace(r.FormValue(common.ParamDomain))
	domain, err := common.ParseDomainName(renderCtx.Domain)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse domain name", "domain", renderCtx.Domain, common.ErrAttr(err))
		renderCtx.DomainError = "Invalid format of domain name."
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	_, ignoreError := r.Form[common.ParamIgnoreError]
	if domainError := s.validateDomainName(ctx, domain, ignoreError); len(domainError) > 0 {
		renderCtx.DomainError = domainError
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	if limitError := s.validatePropertiesLimit(ctx, org, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	property, auditEvent, err := s.Store.Impl().CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:      renderCtx.Name,
		CreatorID: db.Int(user.ID),
		Domain:    domain,
		Level:     db.Int2(int16(common.DifficultyLevelSmall)),
		Growth:    dbgen.DifficultyGrowthMedium,
	}, org)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	dashboardURL := s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertyEndpoint, s.IDHasher.Encrypt(int(property.ID)))
	dashboardURL += fmt.Sprintf("?%s=integrations", common.ParamTab)
	common.Redirect(dashboardURL, http.StatusOK, w, r)

	s.Store.AuditLog().RecordEvent(ctx, auditEvent)
}

func (s *Server) getPropertyStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	// we fetch full org and property to verify parameters as they should be cached anyways, if correct
	org, err := s.Org(user, r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	property, err := s.Property(org, r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	periodStr := r.PathValue(common.ParamPeriod)
	var period common.TimePeriod
	switch periodStr {
	case "24h":
		period = common.TimePeriodToday
	case "7d":
		period = common.TimePeriodWeek
	case "30d":
		period = common.TimePeriodMonth
	case "1y":
		period = common.TimePeriodYear
	default:
		slog.ErrorContext(ctx, "Incorrect period argument", "period", periodStr)
		period = common.TimePeriodToday
	}

	type point struct {
		Date  int64 `json:"x"`
		Value int   `json:"y"`
	}

	requested := []*point{}
	verified := []*point{}

	if stats, err := s.TimeSeries.RetrievePropertyStatsByPeriod(ctx, org.ID, property.ID, period); err == nil {
		anyNonZero := false
		for _, st := range stats {
			if (st.RequestsCount > 0) || (st.VerifiesCount > 0) {
				anyNonZero = true
			}
			requested = append(requested, &point{Date: st.Timestamp.Unix(), Value: st.RequestsCount})
			verified = append(verified, &point{Date: st.Timestamp.Unix(), Value: st.VerifiesCount})
		}

		// we want to show "No data available" on the client
		if !anyNonZero {
			requested = []*point{}
			verified = []*point{}
		}
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve property stats", common.ErrAttr(err))
	}

	response := struct {
		Requested []*point `json:"requested"`
		Verified  []*point `json:"verified"`
	}{
		Requested: requested,
		Verified:  verified,
	}

	common.SendJSONResponse(ctx, w, response, common.NoCacheHeaders)
}

func (s *Server) getOrgProperty(w http.ResponseWriter, r *http.Request) (*propertyDashboardRenderContext, *dbgen.Property, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, nil, err
	}

	org, err := s.Org(user, r)
	if err != nil {
		return nil, nil, err
	}

	property, err := s.Property(org, r)
	if err != nil {
		return nil, nil, err
	}

	renderCtx := &propertyDashboardRenderContext{
		CsrfRenderContext:    s.CreateCsrfContext(user),
		CaptchaRenderContext: s.createDemoCaptchaRenderContext(strings.ReplaceAll(propertySettingsPropertyID, "-", "")),
		Property:             propertyToUserProperty(property, s.IDHasher),
		Org:                  orgToUserOrg(org, user.ID, s.IDHasher),
		CanEdit:              (user.ID == org.UserID.Int32) || (user.ID == property.CreatorID.Int32),
	}

	return renderCtx, property, nil
}

func (s *Server) getOrgPropertySettings(w http.ResponseWriter, r *http.Request) (*propertySettingsRenderContext, *common.AuditLogEvent, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, nil, err
	}

	propertyRenderCtx, property, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, nil, err
	}

	renderCtx := &propertySettingsRenderContext{
		propertyDashboardRenderContext: *propertyRenderCtx,
		difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
		Orgs:                           []*userOrg{},
		CanMove:                        false,
	}

	// only property owner can move it around
	if user.ID == property.CreatorID.Int32 {
		if orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user); err == nil {
			renderCtx.Orgs = orgsToUserOrgs(orgs, s.IDHasher)

			for _, org := range orgs {
				if (org.Organization.ID != property.OrgID.Int32) && (org.Level == dbgen.AccessLevelOwner) {
					slog.DebugContext(ctx, "Found at least one other user-owned org", "orgID", org.Organization.ID, "orgName", org.Organization.Name)
					renderCtx.CanMove = true
					break
				}
			}
		}
	}

	renderCtx.Tab = propertySettingsTabIndex

	renderCtx.UpdateLevels()

	auditEvent := newAccessAuditLogEvent(user, db.TableNameProperties, int64(property.ID), property.Name, common.SettingsEndpoint)

	return renderCtx, auditEvent, nil
}

func (s *Server) getPropertyDashboard(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	tabParam := r.URL.Query().Get(common.ParamTab)
	slog.Log(ctx, common.LevelTrace, "Property tab was requested", "tab", tabParam)
	var model Model
	var derr error
	var event *common.AuditLogEvent
	switch tabParam {
	case common.IntegrationsEndpoint:
		if integrationsCtx, err := s.getPropertyIntegrations(w, r); err == nil {
			model = integrationsCtx
		} else {
			derr = err
		}
	case common.SettingsEndpoint:
		if renderCtx, ae, err := s.getOrgPropertySettings(w, r); err == nil {
			model = renderCtx
			event = ae
		} else {
			derr = err
		}
	case common.EventsEndpoint:
		if auditLogsCtx, ae, err := s.getPropertyAuditLogs(w, r); err == nil {
			model = auditLogsCtx
			event = ae
		} else {
			derr = err
		}
	default:
		if (tabParam != common.ReportsEndpoint) && (tabParam != "") {
			slog.ErrorContext(ctx, "Unknown tab requested", "tab", tabParam)
		}
		if renderCtx, _, err := s.getOrgProperty(w, r); err == nil {
			renderCtx.Tab = 0
			model = renderCtx
		} else {
			derr = err
		}
	}

	if derr != nil {
		return nil, derr
	}

	return &ViewModel{Model: model, View: propertyDashboardTemplate, AuditEvent: event}, nil
}

func (s *Server) getPropertyReportsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	renderCtx, _, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: propertyDashboardReportsTemplate}, nil
}

func (s *Server) getPropertySettingsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	renderCtx, auditEvent, err := s.getOrgPropertySettings(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: propertyDashboardSettingsTemplate, AuditEvent: auditEvent}, nil
}

func (s *Server) getPropertyIntegrations(w http.ResponseWriter, r *http.Request) (*propertyIntegrationsRenderContext, error) {
	dashboardCtx, property, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, err
	}

	renderCtx := &propertyIntegrationsRenderContext{
		propertyDashboardRenderContext: *dashboardCtx,
		Sitekey:                        db.UUIDToSiteKey(property.ExternalID),
	}

	renderCtx.Tab = propertyIntegrationsTabIndex

	return renderCtx, nil
}

func (s *Server) getPropertyIntegrationsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx, err := s.getPropertyIntegrations(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: ctx, View: propertyDashboardIntegrationsTemplate}, nil
}

func (s *Server) newPropertyAuditLogs(ctx context.Context, user *dbgen.User, logs []*dbgen.GetPropertyAuditLogsRow) []*userAuditLog {
	result := make([]*userAuditLog, 0, len(logs))

	for _, log := range logs {
		if ul, err := s.newUserAuditLog(ctx, &log.AuditLog); err == nil {
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

func (s *Server) getPropertyAuditLogsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx, auditEvent, err := s.getPropertyAuditLogs(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: ctx, View: propertyDashboardAuditLogsTemplate, AuditEvent: auditEvent}, nil
}

func (s *Server) putProperty(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
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

	renderCtx, _, err := s.getOrgPropertySettings(w, r)
	if err != nil {
		return nil, err
	}

	// should hit cache right away
	org, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	property, err := s.Property(org, r)
	if err != nil {
		return nil, err
	}

	if !renderCtx.CanEdit {
		slog.WarnContext(ctx, "Insufficient permissions to edit property", "userID", user.ID, "orgUserID", org.UserID.Int32,
			"propUserID", property.CreatorID.Int32)
		renderCtx.ErrorMessage = "Insufficient permissions to update settings."
		return &ViewModel{Model: renderCtx, View: propertyDashboardSettingsTemplate}, nil
	}

	name := r.FormValue(common.ParamName)
	if name != property.Name {
		if nameError := s.validatePropertyName(ctx, name, org); len(nameError) > 0 {
			renderCtx.NameError = nameError
			renderCtx.Property.Name = name
			return &ViewModel{Model: renderCtx, View: propertyDashboardSettingsTemplate}, nil
		}
	}

	difficulty := difficultyLevelFromValue(ctx, r.FormValue(common.ParamDifficulty), renderCtx.MinLevel, renderCtx.MaxLevel)
	growth := growthLevelFromIndex(ctx, r.FormValue(common.ParamGrowth))
	validityInterval := validityIntervalFromIndex(ctx, r.FormValue(common.ParamValidityInterval))
	_, allowSubdomains := r.Form[common.ParamAllowSubdomains]
	_, allowLocalhost := r.Form[common.ParamAllowLocalhost]

	var maxReplayCount int32 = 1
	if _, allowReplay := r.Form[common.ParamAllowReplay]; allowReplay {
		maxReplayCount = parseMaxReplayCount(ctx, r.FormValue(common.ParamMaxReplayCount))
	}

	var auditEvent *common.AuditLogEvent

	if (name != property.Name) ||
		(int16(difficulty) != property.Level.Int16) ||
		(growth != property.Growth) ||
		(validityInterval != property.ValidityInterval) ||
		(maxReplayCount != property.MaxReplayCount) ||
		(allowSubdomains != property.AllowSubdomains) ||
		(allowLocalhost != property.AllowLocalhost) {
		params := &dbgen.UpdatePropertyParams{
			ID:               property.ID,
			Name:             name,
			Level:            db.Int2(int16(difficulty)),
			Growth:           growth,
			ValidityInterval: validityInterval,
			AllowSubdomains:  allowSubdomains,
			AllowLocalhost:   allowLocalhost,
			MaxReplayCount:   maxReplayCount,
		}

		var updatedProperty *dbgen.Property
		if updatedProperty, auditEvent, err = s.Store.Impl().UpdateProperty(ctx, property, org, params); err != nil {
			renderCtx.ErrorMessage = "Failed to update settings. Please try again."
		} else {
			slog.InfoContext(ctx, "Edited property", "propID", property.ID, "orgID", org.ID)
			renderCtx.SuccessMessage = "Settings were updated"
			renderCtx.Property = propertyToUserProperty(updatedProperty, s.IDHasher)
		}
	}

	return &ViewModel{Model: renderCtx, View: propertyDashboardSettingsTemplate, AuditEvent: auditEvent}, nil
}

func (s *Server) deleteProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	property, err := s.Property(org, r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	canDelete := (user.ID == org.UserID.Int32) || (user.ID == property.CreatorID.Int32)
	if !canDelete {
		slog.ErrorContext(ctx, "Not enough permissions to delete property", "userID", user.ID,
			"orgUserID", org.UserID.Int32, "propertyUserID", property.CreatorID.Int32)
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	if auditEvent, err := s.Store.Impl().SoftDeleteProperty(ctx, property, org, user); err == nil {
		common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID))), http.StatusOK, w, r)
		s.Store.AuditLog().RecordEvent(ctx, auditEvent)
	} else {
		s.RedirectError(http.StatusInternalServerError, w, r)
	}
}
