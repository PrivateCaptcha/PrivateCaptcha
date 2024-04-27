package portal

import (
	"context"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

const (
	maxNewPropertyFormSizeBytes = 256 * 1024
	createPropertyFormTemplate  = "property-wizard/form.html"
	createOrgFormTemplate       = "org-wizard/form.html"
	propertyDashboardTemplate   = "property/dashboard.html"
	maxPropertyNameLength       = 255
)

var (
	domainRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,61}[a-zA-Z0-9](?:\.[a-zA-Z]{2,})+$`)
)

type propertyWizardRenderContext struct {
	Token       string
	NameError   string
	DomainError string
	CurrentOrg  *userOrg
}

type userProperty struct {
	ID     string
	OrgID  string
	Name   string
	Domain string
}

type orgPropertiesRenderContext struct {
	Properties []*userProperty
	CurrentOrg *userOrg
}

type propertyDashboardRenderContext struct {
	Property *userProperty
	Org      *userOrg
}

func propertyToUserProperty(p *dbgen.Property) *userProperty {
	return &userProperty{
		ID:     strconv.Itoa(int(p.ID)),
		OrgID:  strconv.Itoa(int(p.OrgID.Int32)),
		Name:   p.Name,
		Domain: p.Domain,
	}
}

func propertiesToUserProperties(properties []*dbgen.Property) []*userProperty {
	result := make([]*userProperty, 0, len(properties))

	for _, p := range properties {
		result = append(result, propertyToUserProperty(p))
	}

	return result
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

func (s *Server) getNewOrgProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok || (len(email) == 0) {
		slog.ErrorContext(ctx, "Failed to get user email from context")
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	orgID := ctx.Value(common.OrgIDContextKey).(int)

	org, err := s.Store.RetrieveOrganization(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find org by ID", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	data := &propertyWizardRenderContext{
		Token: s.XSRF.Token(email, actionNewProperty),
		CurrentOrg: &userOrg{
			Name:  org.Name,
			ID:    strconv.Itoa(int(org.ID)),
			Level: "",
		},
	}

	s.render(w, r, "property-wizard/wizard.html", data)
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

func (s *Server) postNewOrgProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxNewPropertyFormSizeBytes)
	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, email, actionNewProperty) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		common.Redirect(s.relURL(common.ExpiredEndpoint), w, r)
		return
	}

	orgID := ctx.Value(common.OrgIDContextKey).(int)
	org, err := s.Store.RetrieveOrganization(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find org by ID", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &propertyWizardRenderContext{
		Token:      s.XSRF.Token(email, actionNewProperty),
		CurrentOrg: orgToUserOrg(org),
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

	_, err = s.Store.CreateProperty(ctx, name, org.ID, domain, difficulty, growth)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	// TODO: Redirect to property page instead of dashboard
	common.Redirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(orgID)), w, r)
}

// TODO: Add min/max points for each of the periods respectively
// so that scaling on charts will be OK
func (s *Server) getRandomPropertyStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period := ctx.Value(common.PeriodContextKey).(string)

	type point struct {
		Date  int64 `json:"x"`
		Value int   `json:"y"`
	}

	requested := []*point{}
	verified := []*point{}

	var interval time.Duration
	var count int

	switch period {
	case "7d":
		count = 7 * (24 / 3) / 2
		interval = 7 * 24 * time.Hour
	case "30d":
		interval = 30 * 24 * time.Hour
		count = 30
	case "6m":
		interval = 6 * 30 * 24 * time.Hour
		count = 6 * (30 / 2) / 2
	case "1y":
		interval = 12 * 30 * 24 * time.Hour
		count = 12 * (30 / 5 / 2)
	default:
		slog.ErrorContext(ctx, "Incorrect period", "period", period)
		interval = 24 * time.Hour
		count = 24 / 2
	}

	step := interval / time.Duration(count)

	for i := 0; i < count; i++ {
		n := rand.Intn(100000)
		requested = append(requested, &point{time.Now().Add(time.Duration(-i) * step).Unix(), n})
		verified = append(verified, &point{time.Now().Add(time.Duration(-i) * step).Unix(), rand.Intn(n)})
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

func (s *Server) getPropertyDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgID := ctx.Value(common.OrgIDContextKey).(int)
	propertyID := ctx.Value(common.PropertyIDContextKey).(int)

	property, err := s.Store.RetrieveProperty(ctx, int32(propertyID))
	if (err != nil) || (int(property.OrgID.Int32) != orgID) {
		slog.ErrorContext(ctx, "Failed to find property", "orgID", orgID, "propID", propertyID, common.ErrAttr(err))
		s.redirectError(http.StatusNotFound, w, r)
		return
	}

	org, err := s.Store.RetrieveOrganization(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find org", "orgID", orgID, common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &propertyDashboardRenderContext{
		Property: propertyToUserProperty(property),
		Org:      orgToUserOrg(org),
	}
	s.render(w, r, propertyDashboardTemplate, renderCtx)
}
