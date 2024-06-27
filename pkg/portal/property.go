package portal

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
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
)

var (
	domainRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9](?:\.[a-zA-Z]{2,})+$`)
)

type propertyWizardRenderContext struct {
	csrfRenderContext
	NameError   string
	DomainError string
	CurrentOrg  *userOrg
}

type userProperty struct {
	ID     string
	OrgID  string
	Name   string
	Domain string
	Level  int
	Growth int
}

type orgPropertiesRenderContext struct {
	csrfRenderContext
	Properties []*userProperty
	CurrentOrg *userOrg
}

type propertyDashboardRenderContext struct {
	alertRenderContext
	csrfRenderContext
	Property  *userProperty
	Org       *userOrg
	NameError string
	Tab       int
	CanEdit   bool
}

func propertyToUserProperty(p *dbgen.Property) *userProperty {
	return &userProperty{
		ID:     strconv.Itoa(int(p.ID)),
		OrgID:  strconv.Itoa(int(p.OrgID.Int32)),
		Name:   p.Name,
		Domain: p.Domain,
		Level:  difficultyLevelToIndex(p.Level),
		Growth: growthLevelToIndex(p.Growth),
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
	case dbgen.DifficultyGrowthSlow:
		return 0
	case dbgen.DifficultyGrowthMedium:
		return 1
	case dbgen.DifficultyGrowthFast:
		return 2
	default:
		return 1
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
		return dbgen.DifficultyGrowthSlow
	case 1:
		return dbgen.DifficultyGrowthMedium
	case 2:
		return dbgen.DifficultyGrowthFast
	default:
		slog.WarnContext(ctx, "Invalid growth level index", "index", i)
		return dbgen.DifficultyGrowthMedium
	}
}

func difficultyLevelToIndex(level dbgen.DifficultyLevel) int {
	switch level {
	case dbgen.DifficultyLevelSmall:
		return 0
	case dbgen.DifficultyLevelMedium:
		return 1
	case dbgen.DifficultyLevelHigh:
		return 2
	default:
		return 1
	}
}

func difficultyLevelFromIndex(ctx context.Context, index string) dbgen.DifficultyLevel {
	i, err := strconv.Atoi(index)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert difficulty level", "value", index, common.ErrAttr(err))
		return dbgen.DifficultyLevelMedium
	}

	switch i {
	case 0:
		return dbgen.DifficultyLevelSmall
	case 1:
		return dbgen.DifficultyLevelMedium
	case 2:
		return dbgen.DifficultyLevelHigh
	default:
		slog.WarnContext(ctx, "Invalid difficulty level index", "index", i)
		return dbgen.DifficultyLevelMedium
	}
}

func (s *Server) getNewOrgProperty(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	user, err := s.sessionUser(w, r)
	if err != nil {
		return nil, "", err
	}

	org, err := s.org(r)
	if err != nil {
		return nil, "", err
	}

	data := &propertyWizardRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(user.Email),
		},
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

	if !domainRegexp.MatchString(domain) {
		slog.WarnContext(ctx, "Failed to validate domain name", "domain", domain)
		return "Domain name is not valid."
	}

	const timeout = 5 * time.Second
	rctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	var r net.Resolver
	names, err := r.LookupIPAddr(rctx, domain)
	if err == nil && len(names) > 0 {
		slog.DebugContext(ctx, "Resolved domain name", "domain", domain, "ips", len(names), "first", names[0])
		return ""
	}

	if err != nil {
		slog.ErrorContext(ctx, "Failed to resolve domain name", "domain", domain, common.ErrAttr(err))
	}

	return "Failed to resolve domain name."
}

// This one cannot be "MVC" function because it redirects in case of success
func (s *Server) postNewOrgProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(w, r)
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

	org, err := s.org(r)
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &propertyWizardRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(user.Email),
		},
		CurrentOrg: orgToUserOrg(org, user.ID),
	}

	name := r.FormValue(common.ParamName)
	if nameError := s.validatePropertyName(ctx, name, org.ID); len(nameError) > 0 {
		renderCtx.NameError = nameError
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	domain := r.FormValue(common.ParamDomain)
	if domainError := s.validateDomainName(ctx, domain); len(domainError) > 0 {
		renderCtx.DomainError = domainError
		s.render(w, r, createPropertyFormTemplate, renderCtx)
		return
	}

	difficulty := difficultyLevelFromIndex(ctx, r.FormValue(common.ParamDifficulty))
	growth := growthLevelFromIndex(ctx, r.FormValue(common.ParamGrowth))

	property, err := s.Store.CreateNewProperty(ctx, &dbgen.CreatePropertyParams{
		Name:       name,
		OrgID:      db.Int(org.ID),
		CreatorID:  db.Int(user.ID),
		OrgOwnerID: org.UserID,
		Domain:     domain,
		Level:      difficulty,
		Growth:     growth,
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	dashboardURL := s.partsURL(common.OrgEndpoint, strconv.Itoa(int(org.ID)), common.PropertyEndpoint, strconv.Itoa(int(property.ID)))
	dashboardURL += fmt.Sprintf("?%s=integrations", common.ParamTab)
	common.Redirect(dashboardURL, w, r)
}

func (s *Server) getPropertyStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orgID, err := s.orgID(r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	propertyID, err := s.propertyID(r)
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

	// TODO: Verify org and property ID against cache before accessing ClickHouse
	if stats, err := s.TimeSeries.RetrievePropertyStats(ctx, int32(orgID), int32(propertyID), period); err == nil {
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

	// TODO: add CORS headers for chart response
	common.SendJSONResponse(ctx, w, response, map[string]string{})
}

func (s *Server) getOrgProperty(w http.ResponseWriter, r *http.Request) (*propertyDashboardRenderContext, error) {
	ctx := r.Context()

	org, err := s.org(r)
	if err != nil {
		return nil, err
	}

	property, err := s.property(r)
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return nil, err
	}

	if property.OrgID.Int32 != org.ID {
		slog.ErrorContext(ctx, "Property org does not match", "propertyOrgID", property.OrgID.Int32, "orgID", org.ID)
		return nil, errInvalidPathArg
	}

	user, err := s.sessionUser(w, r)
	if err != nil {
		return nil, err
	}

	renderCtx := &propertyDashboardRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(user.Email),
		},
		Property: propertyToUserProperty(property),
		Org:      orgToUserOrg(org, user.ID),
		CanEdit:  (user.ID == org.UserID.Int32) || (user.ID == property.CreatorID.Int32),
	}
	return renderCtx, nil
}

func (s *Server) getPropertyDashboard(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	tabParam := r.URL.Query().Get(common.ParamTab)
	var tab int
	switch tabParam {
	case "integrations":
		tab = 1
	case "settings":
		tab = 2
	default:
		if tabParam != "reports" {
			slog.ErrorContext(ctx, "Unknown tab requested", "tab", tabParam)
		}
		tab = 0
	}

	renderCtx, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, "", err
	}
	renderCtx.Tab = tab

	return renderCtx, propertyDashboardTemplate, nil
}

func (s *Server) getPropertyReports(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	renderCtx, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, "", err
	}

	return renderCtx, propertyDashboardReportsTemplate, nil
}

func (s *Server) getPropertySettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	renderCtx, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, "", err
	}

	return renderCtx, propertyDashboardSettingsTemplate, nil
}

func (s *Server) getPropertyIntegrations(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	renderCtx, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, "", err
	}

	return renderCtx, propertyDashboardIntegrationsTemplate, nil
}

func (s *Server) putProperty(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(w, r)
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", errInvalidRequestArg
	}

	org, err := s.org(r)
	if err != nil {
		return nil, "", err
	}

	property, err := s.property(r)
	if err != nil {
		return nil, "", err
	}

	if property.OrgID.Int32 != org.ID {
		slog.ErrorContext(ctx, "Property org does not match", "propertyOrgID", property.OrgID.Int32, "orgID", org.ID)
		return nil, "", errInvalidRequestArg
	}

	renderCtx := &propertyDashboardRenderContext{
		csrfRenderContext: csrfRenderContext{
			Token: s.XSRF.Token(user.Email),
		},
		Property: propertyToUserProperty(property),
		Org:      orgToUserOrg(org, user.ID),
		Tab:      2, // settings
		CanEdit:  (user.ID == org.UserID.Int32) || (user.ID == property.CreatorID.Int32),
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

	difficulty := difficultyLevelFromIndex(ctx, r.FormValue(common.ParamDifficulty))
	growth := growthLevelFromIndex(ctx, r.FormValue(common.ParamGrowth))

	if (name != property.Name) || (difficulty != property.Level) || (growth != property.Growth) {
		if updatedProperty, err := s.Store.UpdateProperty(ctx, property.ID, name, difficulty, growth); err != nil {
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

	org, err := s.org(r)
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	property, err := s.property(r)
	if err != nil {
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	if property.OrgID.Int32 != int32(org.ID) {
		slog.ErrorContext(ctx, "Property org does not match", "propertyOrgID", property.OrgID.Int32, "orgID", org.ID)
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	user, err := s.sessionUser(w, r)
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
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
		common.Redirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(int(org.ID))), w, r)
	} else {
		s.redirectError(http.StatusInternalServerError, w, r)
	}
}
