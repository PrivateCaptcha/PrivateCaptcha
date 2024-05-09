package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	errInvalidSession = errors.New("session contains invalid data")
	maxOrgNameLength  = 255
	errNoOrgs         = errors.New("user has no organizations")
)

const (
	orgPropertiesTemplate = "portal/org-dashboard.html"
	orgSettingsTemplate   = "portal/org-settings.html"
)

type orgSettingsRenderContext struct {
	CurrentOrg    *userOrg
	Token         string
	NameError     string
	UpdateMessage string
	UpdateError   string
	CanEdit       bool
}

type userOrg struct {
	Name  string
	ID    string
	Level string
}

type orgDashboardRenderContext struct {
	Orgs       []*userOrg
	CurrentOrg *userOrg
	// shortened from CurrentOrgProperties for simplicity
	Properties []*userProperty
}

type orgWizardRenderContext struct {
	Token     string
	NameError string
}

func orgToUserOrg(org *dbgen.Organization) *userOrg {
	return &userOrg{
		Name: org.Name,
		ID:   strconv.Itoa(int(org.ID)),
	}
}

func orgsToUserOrgs(orgs []*dbgen.GetUserOrganizationsRow) []*userOrg {
	result := make([]*userOrg, 0, len(orgs))
	for _, org := range orgs {
		result = append(result, &userOrg{
			Name:  org.Organization.Name,
			ID:    strconv.Itoa(int(org.Organization.ID)),
			Level: org.Level,
		})
	}
	return result
}

func (s *Server) getNewOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok || (len(email) == 0) {
		slog.ErrorContext(ctx, "Failed to get user email from context")
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	data := &orgWizardRenderContext{
		Token: s.XSRF.Token(email, actionNewOrg),
	}

	s.render(w, r, "org-wizard/wizard.html", data)
}

func (s *Server) validateOrgName(ctx context.Context, name string, userID int32) string {
	if (len(name) == 0) || (len(name) > maxOrgNameLength) {
		slog.WarnContext(ctx, "Name length is invalid", "length", len(name))

		if len(name) == 0 {
			return "Name cannot be empty."
		} else {
			return "Name is too long."
		}
	}

	if _, err := s.Store.FindOrg(ctx, name, userID); err != db.ErrRecordNotFound {
		slog.WarnContext(ctx, "Org already exists", "name", name, common.ErrAttr(err))
		return "Organization with this name already exists."
	}

	return ""
}

func (s *Server) postNewOrg(w http.ResponseWriter, r *http.Request) {
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
	if !s.XSRF.VerifyToken(token, email, actionNewOrg) {
		slog.WarnContext(ctx, "Failed to verify CSRF token")
		common.Redirect(s.relURL(common.ExpiredEndpoint), w, r)
		return
	}

	renderCtx := &orgWizardRenderContext{
		Token: s.XSRF.Token(email, actionNewOrg),
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	name := r.FormValue(common.ParamName)
	if nameError := s.validateOrgName(ctx, name, user.ID); len(nameError) > 0 {
		renderCtx.NameError = nameError
		s.render(w, r, createOrgFormTemplate, renderCtx)
		return
	}

	org, err := s.Store.CreateNewOrganization(ctx, name, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create organization", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	common.Redirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(int(org.ID))), w, r)
}

func (s *Server) createOrgDashboardContext(ctx context.Context, orgID int32, sess *common.Session) (*orgDashboardRenderContext, error) {
	slog.DebugContext(ctx, "Creating org dashboard context", "orgID", orgID)

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok || (len(email) == 0) {
		slog.ErrorContext(ctx, "Failed to get user email from context")
		return nil, errInvalidSession
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		return nil, err
	}

	orgs, err := s.Store.FindUserOrganizations(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user orgs", common.ErrAttr(err))
		return nil, err
	}

	if len(orgs) == 0 {
		slog.WarnContext(ctx, "User has no organizations")
		return nil, errNoOrgs
	}

	renderCtx := &orgDashboardRenderContext{
		Orgs: orgsToUserOrgs(orgs),
	}

	idx := -1
	if orgID >= 0 {
		idx = slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool { return o.Organization.ID == orgID })
		slog.Log(ctx, common.LevelTrace, "Searched in user organizations", "index", idx, "orgID", orgID)
	}

	if idx >= 0 {
		renderCtx.CurrentOrg = renderCtx.Orgs[idx]
		slog.DebugContext(ctx, "Selected current org from path", "index", idx)
	} else if len(renderCtx.Orgs) > 0 {
		earliestIdx := 0
		earliestDate := time.Now()

		for i, o := range orgs {
			if (o.Level == string(dbgen.AccessLevelOwner)) && o.Organization.CreatedAt.Time.Before(earliestDate) {
				earliestIdx = i
				earliestDate = o.Organization.CreatedAt.Time
			}
		}

		idx = earliestIdx
		renderCtx.CurrentOrg = renderCtx.Orgs[earliestIdx]
		slog.DebugContext(ctx, "Selected current org as earliest owned", "index", idx)
	} else {
		renderCtx.CurrentOrg = &userOrg{ID: "-1"}
	}

	if (0 <= idx) && (idx < len(orgs)) {
		properties, err := s.Store.RetrieveOrgProperties(ctx, orgs[idx].Organization.ID)
		if err == nil {
			renderCtx.Properties = propertiesToUserProperties(ctx, properties)
		}
	}

	return renderCtx, nil
}

func (s *Server) getPortal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	org, ok := ctx.Value(common.OrgIDContextKey).(int)
	if !ok {
		slog.WarnContext(ctx, "Org path argument is not set in context")
		org = -1
	}

	sess := s.Session.SessionStart(w, r)
	renderCtx, err := s.createOrgDashboardContext(ctx, int32(org), sess)
	if err != nil {
		if (org == -1) && (err == errNoOrgs) {
			common.Redirect(s.partsURL(common.OrgEndpoint, common.NewEndpoint), w, r)
		} else {
			s.redirectError(http.StatusInternalServerError, w, r)
		}
		return
	}

	s.render(w, r, "portal/portal.html", renderCtx)
}

func (s *Server) getOrgDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgID := ctx.Value(common.OrgIDContextKey).(int)

	properties, err := s.Store.RetrieveOrgProperties(ctx, int32(orgID))
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &orgPropertiesRenderContext{
		Properties: propertiesToUserProperties(ctx, properties),
		CurrentOrg: &userOrg{
			ID: strconv.Itoa(orgID),
		},
	}
	s.render(w, r, orgPropertiesTemplate, renderCtx)
}

func (s *Server) getOrgSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	orgID := ctx.Value(common.OrgIDContextKey).(int)
	org, err := s.Store.RetrieveOrganization(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find org by ID", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	data := &orgSettingsRenderContext{
		CurrentOrg: orgToUserOrg(org),
		Token:      s.XSRF.Token(email, actionOrg),
		CanEdit:    org.UserID.Int32 == user.ID,
	}
	s.render(w, r, orgSettingsTemplate, data)
}

func (s *Server) putOrg(w http.ResponseWriter, r *http.Request) {
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
	if !s.XSRF.VerifyToken(token, email, actionOrg) {
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

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &orgSettingsRenderContext{
		CurrentOrg: orgToUserOrg(org),
		Token:      s.XSRF.Token(email, actionOrg),
		CanEdit:    org.UserID.Int32 == user.ID,
	}

	if org.UserID.Int32 != user.ID {
		renderCtx.UpdateError = "Does not have permissions to update settings."
		s.render(w, r, orgSettingsTemplate, renderCtx)
		return
	}

	name := r.FormValue(common.ParamName)
	if name != org.Name {
		if nameError := s.validateOrgName(ctx, name, user.ID); len(nameError) > 0 {
			renderCtx.NameError = nameError
			s.render(w, r, orgSettingsTemplate, renderCtx)
			return
		}

		if updatedOrg, err := s.Store.UpdateOrganization(ctx, org.ID, name); err != nil {
			renderCtx.UpdateError = "Failed to update settings. Please try again."
		} else {
			renderCtx.UpdateMessage = "Settings were updated"
			renderCtx.CurrentOrg = orgToUserOrg(updatedOrg)
		}
	}

	s.render(w, r, orgSettingsTemplate, renderCtx)
}

func (s *Server) deleteOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	orgID := ctx.Value(common.OrgIDContextKey).(int)
	org, err := s.Store.RetrieveOrganization(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find org by ID", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	if org.UserID.Int32 != user.ID {
		slog.ErrorContext(ctx, "Does not have permissions to delete org", "userID", user.ID, "orgUserID", org.UserID.Int32)
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	if err := s.Store.SoftDeleteOrganization(ctx, int32(orgID), user.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete organization", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	common.Redirect(s.relURL("/"), w, r)
}
