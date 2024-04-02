package portal

import (
	"context"
	"log/slog"
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

type orgPropertiesRenderContext struct {
	Properties []interface{}
	CurrentOrg *userOrg
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

func (s *Server) getOrgProperties(w http.ResponseWriter, r *http.Request) {
	data := &orgPropertiesRenderContext{
		Properties: []interface{}{},
		CurrentOrg: &userOrg{
			ID: r.PathValue("org"),
		},
	}
	s.render(w, r, "portal/properties.html", data)
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
			Name:  org.OrgName,
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
		s.htmxRedirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxNewPropertyFormSizeBytes)
	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusBadRequest, w, r)
		return
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, email, actionNewProperty) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		s.htmxRedirect(s.relURL(common.ExpiredEndpoint), w, r)
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

	_, err = s.Store.CreateProperty(ctx, name, org.ID, difficulty, growth)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create property", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	// TODO: Redirect to property page instead of dashboard
	s.htmxRedirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(orgID)), w, r)
}
