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

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
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
	propertyWizardTemplate                = "property-wizard/wizard.html"
	maxPropertyNameLength                 = 255
	propertySettingsPropertyID            = "371d58d2-f8b9-44e2-ac2e-e61253274bae"
	propertySettingsTabIndex              = 2
	propertyIntegrationsTabIndex          = 1
)

type difficultyLevelsRenderContext struct {
	EasyLevel   int
	NormalLevel int
	HardLevel   int
}

type propertyWizardRenderContext struct {
	csrfRenderContext
	alertRenderContext
	NameError   string
	DomainError string
	CurrentOrg  *userOrg
}

type userProperty struct {
	ID              string
	OrgID           string
	Name            string
	Domain          string
	Level           int
	Growth          int
	AllowSubdomains bool
	AllowLocalhost  bool
}

type orgPropertiesRenderContext struct {
	csrfRenderContext
	Properties []*userProperty
	CurrentOrg *userOrg
}

type propertyDashboardRenderContext struct {
	alertRenderContext
	csrfRenderContext
	// scripts.html is shared so captcha context has to be shared too
	captchaRenderContext
	Property  *userProperty
	Org       *userOrg
	NameError string
	Tab       int
	CanEdit   bool
}

type propertySettingsRenderContext struct {
	propertyDashboardRenderContext
	difficultyLevelsRenderContext
	MinLevel int
	MaxLevel int
}

func (pc *propertySettingsRenderContext) UpdateLevels() {
	const epsilon = common.DifficultyDelta

	pc.MinLevel = max(1, min(pc.EasyLevel-epsilon, int(pc.Property.Level)-epsilon))
	pc.MaxLevel = min(int(common.MaxDifficultyLevel), max(pc.HardLevel+epsilon, int(pc.Property.Level)+epsilon))
}

type propertyIntegrationsRenderContext struct {
	propertyDashboardRenderContext
	Sitekey string
}

func createDifficultyLevelsRenderContext() difficultyLevelsRenderContext {
	return difficultyLevelsRenderContext{
		EasyLevel:   int(common.DifficultyLevelSmall),
		NormalLevel: int(common.DifficultyLevelMedium),
		HardLevel:   int(common.DifficultyLevelHigh),
	}
}

func propertyToUserProperty(p *dbgen.Property) *userProperty {
	return &userProperty{
		ID:              strconv.Itoa(int(p.ID)),
		OrgID:           strconv.Itoa(int(p.OrgID.Int32)),
		Name:            p.Name,
		Domain:          p.Domain,
		Level:           int(p.Level.Int16),
		Growth:          growthLevelToIndex(p.Growth),
		AllowSubdomains: p.AllowSubdomains,
		AllowLocalhost:  p.AllowLocalhost,
	}
}

func propertiesToUserProperties(ctx context.Context, properties []*dbgen.Property) []*userProperty {
	result := make([]*userProperty, 0, len(properties))

	for _, p := range properties {
		if p.DeletedAt.Valid {
			slog.WarnContext(ctx, "Skipping soft-deleted property", "propID", p.ID, "orgID", p.OrgID, "deleteAt", p.DeletedAt)
			continue
		}

		result = append(result, propertyToUserProperty(p))
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

func difficultyLevelFromValue(ctx context.Context, value string) common.DifficultyLevel {
	i, err := strconv.Atoi(value)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert difficulty level", "value", value, common.ErrAttr(err))
		return common.DifficultyLevelMedium
	}

	if (i <= 0) || (i > int(common.MaxDifficultyLevel)) {
		return common.DifficultyLevelMedium
	}

	return common.DifficultyLevel(i)
}

func (s *Server) getNewOrgProperty(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	org, err := s.org(user.ID, r)
	if err != nil {
		return nil, "", err
	}

	data := &propertyWizardRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
		CurrentOrg: &userOrg{
			Name:  org.Name,
			ID:    strconv.Itoa(int(org.ID)),
			Level: "",
		},
	}

	return data, propertyWizardTemplate, nil
}

func (s *Server) validatePropertyName(ctx context.Context, name string, orgID int32) string {
	if (len(name) == 0) || (len(name) > maxPropertyNameLength) {
		slog.WarnContext(ctx, "Name length is invalid", "length", len(name))

		if len(name) == 0 {
			return "Name cannot be empty."
		} else {
			return "Name is too long."
		}
	}

	if _, err := s.Store.FindOrgProperty(ctx, name, orgID); err != db.ErrRecordNotFound {
		slog.WarnContext(ctx, "Property already exists", "name", name, common.ErrAttr(err))
		return "Property with this name already exists."
	}

	return ""
}

func (s *Server) validateDomainName(ctx context.Context, domain string) string {
	if len(domain) == 0 {
		return "Domain name cannot be empty."
	}

	const localhostError = "Localhost is not allowed as a domain"

	if common.IsLocalhost(domain) {
		return localhostError
	}

	_, err := idna.Lookup.ToASCII(domain)
	if err != nil {
		slog.WarnContext(ctx, "Failed to validate domain name", "domain", domain, common.ErrAttr(err))
		return "Domain name is not valid."
	}

	const timeout = 5 * time.Second
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

func (s *Server) validatePropertiesLimit(ctx context.Context, user *dbgen.User) string {
	var subscription *dbgen.Subscription
	var err error

	if user.SubscriptionID.Valid {
		subscription, err = s.Store.RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return ""
		}
	}

	if (subscription == nil) || !billing.IsSubscriptionActive(subscription.Status) {
		return "You need an active subscription to create new properties"
	}

	return ""
}

func (s *Server) echoPuzzle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var level common.DifficultyLevel
	if difficultyParam, err := common.StrPathArg(r, common.ParamDifficulty); err == nil {
		level = difficultyLevelFromValue(ctx, difficultyParam)
	} else {
		slog.ErrorContext(ctx, "Failed to retrieve difficulty argument", common.ErrAttr(err))
		level = common.DifficultyLevelSmall
	}

	sitekey := r.URL.Query().Get(common.ParamSiteKey)
	uuid := db.UUIDFromSiteKey(sitekey)

	puzzle := puzzle.NewPuzzle()
	if err := puzzle.Init(uuid.Bytes, uint8(level)); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	_ = s.PuzzleEngine.Write(ctx, puzzle, w)
}

// This one cannot be "MVC" function because it redirects in case of success
func (s *Server) postNewOrgProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	org, err := s.org(user.ID, r)
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &propertyWizardRenderContext{
		csrfRenderContext:  s.createCsrfContext(user),
		alertRenderContext: alertRenderContext{},
		CurrentOrg:         orgToUserOrg(org, user.ID),
	}

	name := r.FormValue(common.ParamName)
	if nameError := s.validatePropertyName(ctx, name, org.ID); len(nameError) > 0 {
		renderCtx.NameError = nameError
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	paramDomain := strings.TrimSpace(r.FormValue(common.ParamDomain))
	domain, err := common.ParseDomainName(paramDomain)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse domain name", "domain", paramDomain, common.ErrAttr(err))
		renderCtx.DomainError = "Invalid format of domain name"
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}
	if domainError := s.validateDomainName(ctx, domain); len(domainError) > 0 {
		renderCtx.DomainError = domainError
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	if limitError := s.validatePropertiesLimit(ctx, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	property, err := s.Store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       name,
		OrgID:      db.Int(org.ID),
		CreatorID:  db.Int(user.ID),
		OrgOwnerID: org.UserID,
		Domain:     domain,
		Level:      db.Int2(int16(common.DifficultyLevelSmall)),
		Growth:     dbgen.DifficultyGrowthMedium,
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	dashboardURL := s.partsURL(common.OrgEndpoint, strconv.Itoa(int(org.ID)), common.PropertyEndpoint, strconv.Itoa(int(property.ID)))
	dashboardURL += fmt.Sprintf("?%s=integrations", common.ParamTab)
	common.Redirect(dashboardURL, http.StatusOK, w, r)
}

func (s *Server) getPropertyStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	// we fetch full org and property to verify parameters as they should be cached anyways, if correct
	org, err := s.org(user.ID, r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	property, err := s.property(org.ID, r)
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

	if stats, err := s.TimeSeries.RetrievePropertyStats(ctx, org.ID, property.ID, period); err == nil {
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

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, nil, err
	}

	org, err := s.org(user.ID, r)
	if err != nil {
		return nil, nil, err
	}

	property, err := s.property(org.ID, r)
	if err != nil {
		return nil, nil, err
	}

	renderCtx := &propertyDashboardRenderContext{
		csrfRenderContext:    s.createCsrfContext(user),
		captchaRenderContext: s.createDemoCaptchaRenderContext(strings.ReplaceAll(propertySettingsPropertyID, "-", "")),
		Property:             propertyToUserProperty(property),
		Org:                  orgToUserOrg(org, user.ID),
		CanEdit:              (user.ID == org.UserID.Int32) || (user.ID == property.CreatorID.Int32),
	}

	return renderCtx, property, nil
}

func (s *Server) getOrgPropertySettings(w http.ResponseWriter, r *http.Request) (*propertySettingsRenderContext, error) {
	propertyRenderCtx, _, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, err
	}

	renderCtx := &propertySettingsRenderContext{
		propertyDashboardRenderContext: *propertyRenderCtx,
		difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
	}

	renderCtx.Tab = propertySettingsTabIndex

	renderCtx.UpdateLevels()

	return renderCtx, nil
}

func (s *Server) getPropertyDashboard(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	tabParam := r.URL.Query().Get(common.ParamTab)
	var model Model
	var derr error
	switch tabParam {
	case "integrations":
		if integrationsCtx, err := s.getPropertyIntegrations(w, r); err == nil {
			model = integrationsCtx
		} else {
			derr = err
		}
	case "settings":
		if renderCtx, err := s.getOrgPropertySettings(w, r); err == nil {
			model = renderCtx
		} else {
			derr = err
		}
	default:
		if (tabParam != "reports") && (tabParam != "") {
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
		return nil, "", derr
	}

	return model, propertyDashboardTemplate, nil
}

func (s *Server) getPropertyReportsTab(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	renderCtx, _, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, "", err
	}

	return renderCtx, propertyDashboardReportsTemplate, nil
}

func (s *Server) getPropertySettingsTab(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	renderCtx, err := s.getOrgPropertySettings(w, r)
	if err != nil {
		return nil, "", err
	}

	return renderCtx, propertyDashboardSettingsTemplate, nil
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

func (s *Server) getPropertyIntegrationsTab(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx, err := s.getPropertyIntegrations(w, r)
	if err != nil {
		return nil, "", err
	}

	return ctx, propertyDashboardIntegrationsTemplate, nil
}

func (s *Server) putProperty(w http.ResponseWriter, r *http.Request) (Model, string, error) {
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

	renderCtx, err := s.getOrgPropertySettings(w, r)
	if err != nil {
		return nil, "", err
	}

	// should hit cache right away
	org, err := s.org(user.ID, r)
	if err != nil {
		return nil, "", err
	}

	property, err := s.property(org.ID, r)
	if err != nil {
		return nil, "", err
	}

	if !renderCtx.CanEdit {
		slog.WarnContext(ctx, "Insufficient permissions to edit property", "userID", user.ID, "orgUserID", org.UserID.Int32,
			"propUserID", property.CreatorID.Int32)
		renderCtx.ErrorMessage = "Insufficient permissions to update settings."
		return renderCtx, propertyDashboardSettingsTemplate, nil
	}

	name := r.FormValue(common.ParamName)
	if name != property.Name {
		if nameError := s.validatePropertyName(ctx, name, org.ID); len(nameError) > 0 {
			renderCtx.NameError = nameError
			renderCtx.Property.Name = name
			return renderCtx, propertyDashboardSettingsTemplate, nil
		}
	}

	difficulty := difficultyLevelFromValue(ctx, r.FormValue(common.ParamDifficulty))
	growth := growthLevelFromIndex(ctx, r.FormValue(common.ParamGrowth))
	_, allowSubdomains := r.Form[common.ParamAllowSubdomains]
	_, allowLocalhost := r.Form[common.ParamAllowLocalhost]

	if (name != property.Name) ||
		(int16(difficulty) != property.Level.Int16) ||
		(growth != property.Growth) ||
		(allowSubdomains != property.AllowSubdomains) ||
		(allowLocalhost != property.AllowLocalhost) {
		if updatedProperty, err := s.Store.UpdateProperty(ctx, &dbgen.UpdatePropertyParams{
			ID:              property.ID,
			Name:            name,
			Level:           db.Int2(int16(difficulty)),
			Growth:          growth,
			AllowSubdomains: allowSubdomains,
			AllowLocalhost:  allowLocalhost,
		}); err != nil {
			renderCtx.ErrorMessage = "Failed to update settings. Please try again."
		} else {
			slog.DebugContext(ctx, "Edited property", "propID", property.ID, "orgID", org.ID)
			renderCtx.SuccessMessage = "Settings were updated"
			renderCtx.Property = propertyToUserProperty(updatedProperty)
		}
	}

	return renderCtx, propertyDashboardSettingsTemplate, nil
}

func (s *Server) deleteProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, err := s.org(user.ID, r)
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	property, err := s.property(org.ID, r)
	if err != nil {
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	canDelete := (user.ID == org.UserID.Int32) || (user.ID == property.CreatorID.Int32)
	if !canDelete {
		slog.ErrorContext(ctx, "Not enough permissions to delete property", "userID", user.ID,
			"orgUserID", org.UserID.Int32, "propertyUserID", property.CreatorID.Int32)
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	if err := s.Store.SoftDeleteProperty(ctx, property.ID, org.ID); err == nil {
		common.Redirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(int(org.ID))), http.StatusOK, w, r)
	} else {
		s.redirectError(http.StatusInternalServerError, w, r)
	}
}
