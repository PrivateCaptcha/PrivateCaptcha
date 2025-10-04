package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	errInvalidSession = errors.New("session contains invalid data")
	maxOrgNameLength  = 255
	errNoOrgs         = errors.New("user has no organizations")
	stubUserOrg       = &userOrg{ID: "-1"}
)

const (
	orgPropertiesTemplate         = "portal/org-dashboard.html"
	orgSettingsTemplate           = "portal/org-settings.html"
	orgMembersTemplate            = "portal/org-members.html"
	orgWizardTemplate             = "org-wizard/wizard.html"
	portalTemplate                = "portal/portal.html"
	activeSubscriptionForOrgError = "You need an active subscription to create new organizations."
	enterpriseOrgError            = "Creating new organizations is only available in the enterprise edition of Private Captcha."
)

type orgSettingsRenderContext struct {
	AlertRenderContext
	CsrfRenderContext
	CurrentOrg *userOrg
	NameError  string
	CanEdit    bool
}

type orgUser struct {
	Name      string
	ID        string
	Level     string
	CreatedAt string
}

type orgMemberRenderContext struct {
	AlertRenderContext
	CsrfRenderContext
	CurrentOrg *userOrg
	Members    []*orgUser
	CanEdit    bool
}

type userOrg struct {
	Name  string
	ID    string
	Level string
}

type orgDashboardRenderContext struct {
	CsrfRenderContext
	systemNotificationContext
	Orgs       []*userOrg
	CurrentOrg *userOrg
	// shortened from CurrentOrgProperties for simplicity
	Properties []*userProperty
}

type orgWizardRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	NameError string
}

func userToOrgUser(user *dbgen.User, level string) *orgUser {
	return &orgUser{
		Name:      user.Name,
		ID:        strconv.Itoa(int(user.ID)),
		CreatedAt: user.CreatedAt.Time.Format("02 Jan 2006"),
		Level:     level,
	}
}

func usersToOrgUsers(users []*dbgen.GetOrganizationUsersRow) []*orgUser {
	result := make([]*orgUser, 0, len(users))

	for _, user := range users {
		result = append(result, userToOrgUser(&user.User, string(user.Level)))
	}

	return result
}

func orgToUserOrg(org *dbgen.Organization, userID int32) *userOrg {
	uo := &userOrg{
		Name: org.Name,
		ID:   strconv.Itoa(int(org.ID)),
	}

	if org.UserID.Int32 == userID {
		uo.Level = string(dbgen.AccessLevelOwner)
	}

	return uo
}

func orgsToUserOrgs(orgs []*dbgen.GetUserOrganizationsRow) []*userOrg {
	result := make([]*userOrg, 0, len(orgs))
	for _, org := range orgs {
		result = append(result, &userOrg{
			Name:  org.Organization.Name,
			ID:    strconv.Itoa(int(org.Organization.ID)),
			Level: string(org.Level),
		})
	}
	return result
}

func (s *Server) getNewOrg(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgWizardRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
	}

	if !user.SubscriptionID.Valid {
		renderCtx.ErrorMessage = activeSubscriptionForOrgError
	} else if !s.isEnterprise() {
		renderCtx.WarningMessage = enterpriseOrgError
	}

	return renderCtx, orgWizardTemplate, nil
}

func (s *Server) validateOrgName(ctx context.Context, name string, user *dbgen.User) string {
	if (len(name) == 0) || (len(name) > maxOrgNameLength) {
		slog.WarnContext(ctx, "Name length is invalid", "length", len(name))

		if len(name) == 0 {
			return "Name cannot be empty."
		} else {
			return "Name is too long."
		}
	}

	const allowedPunctuation = "'-_&.:()[]"

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
			return "Organization name contains invalid characters."
		}
	}

	if _, err := s.Store.Impl().FindOrg(ctx, name, user); err != db.ErrRecordNotFound {
		slog.WarnContext(ctx, "Org already exists", "name", name, common.ErrAttr(err))
		return "Organization with this name already exists."
	}

	return ""
}

func (s *Server) createOrgDashboardContext(ctx context.Context, orgID int32, sess *session.Session) (*orgDashboardRenderContext, error) {
	slog.DebugContext(ctx, "Creating org dashboard context", "orgID", orgID)

	user, err := s.SessionUser(ctx, sess)
	if err != nil {
		return nil, err
	}

	orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user)
	if err != nil {
		return nil, err
	}

	if len(orgs) == 0 {
		slog.WarnContext(ctx, "User has no organizations")
		return nil, errNoOrgs
	}

	idx := -1
	if orgID != -1 {
		idx = slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool { return o.Organization.ID == orgID })
		if idx == -1 {
			slog.WarnContext(ctx, "Org is not found in user orgs", "orgID", orgID, "userID", user.ID)
			return nil, errInvalidPathArg
		}
	}

	renderCtx := &orgDashboardRenderContext{
		CsrfRenderContext:         s.CreateCsrfContext(user),
		systemNotificationContext: s.createSystemNotificationContext(ctx, sess),
		Orgs:                      orgsToUserOrgs(orgs),
		Properties:                []*userProperty{},
		CurrentOrg:                stubUserOrg,
	}

	if idx >= 0 {
		renderCtx.CurrentOrg = renderCtx.Orgs[idx]
		slog.DebugContext(ctx, "Selected current org from path", "index", idx)
	} else if len(renderCtx.Orgs) > 0 {
		earliestIdx := 0
		earliestDate := time.Now()

		for i, o := range orgs {
			if (o.Level == dbgen.AccessLevelOwner) && o.Organization.CreatedAt.Time.Before(earliestDate) {
				earliestIdx = i
				earliestDate = o.Organization.CreatedAt.Time
			}
		}

		idx = earliestIdx
		renderCtx.CurrentOrg = renderCtx.Orgs[earliestIdx]
		slog.DebugContext(ctx, "Selected current org as earliest owned", "index", idx)
	}

	if (0 <= idx) && (idx < len(orgs)) {
		if orgs[idx].Level != dbgen.AccessLevelInvited {
			if properties, err := s.Store.Impl().RetrieveOrgProperties(ctx, &orgs[idx].Organization); err == nil {
				renderCtx.Properties = propertiesToUserProperties(ctx, properties)
			}
		}
	}

	return renderCtx, nil
}

// This cannot be "MVC" function since we're redirecting user to create new org if needed
func (s *Server) getPortal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Sessions.SessionStart(w, r)

	orgID, _, err := common.IntPathArg(r, common.ParamOrg)
	if err != nil {
		slog.WarnContext(ctx, "Org path argument is missing", common.ErrAttr(err))
		orgID = -1
	}

	renderCtx, err := s.createOrgDashboardContext(ctx, int32(orgID), sess)
	if err != nil {
		if (orgID == -1) && (err == errNoOrgs) {
			common.Redirect(s.PartsURL(common.OrgEndpoint, common.NewEndpoint), http.StatusOK, w, r)
		} else if err == errInvalidSession {
			slog.WarnContext(ctx, "Inconsistent user session found")
			s.Sessions.SessionDestroy(w, r)
			common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
		} else if err == errInvalidPathArg {
			s.RedirectError(http.StatusBadRequest, w, r)
		} else {
			s.RedirectError(http.StatusInternalServerError, w, r)
		}
		return
	}

	s.render(w, r, portalTemplate, renderCtx)
}

func (s *Server) getOrgDashboard(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	org, err := s.Org(user, r)
	if err != nil {
		return nil, "", err
	}

	properties, err := s.Store.Impl().RetrieveOrgProperties(ctx, org)
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgPropertiesRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		Properties:        propertiesToUserProperties(ctx, properties),
	}

	return renderCtx, orgPropertiesTemplate, nil
}

func (s *Server) getOrgMembers(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	org, err := s.Org(user, r)
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgMemberRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		CanEdit:           org.UserID.Int32 == user.ID,
	}

	if user.ID != org.UserID.Int32 {
		slog.WarnContext(ctx, "Fetching org members as not an owner", "userID", user.ID)
		return renderCtx, orgMembersTemplate, nil
	}

	members, err := s.Store.Impl().RetrieveOrganizationUsers(ctx, org)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return nil, "", err
	}

	renderCtx.Members = usersToOrgUsers(members)

	return renderCtx, orgMembersTemplate, nil
}

func (s *Server) getOrgSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	org, err := s.Org(user, r)
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgSettingsRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		CanEdit:           org.UserID.Int32 == user.ID,
	}

	return renderCtx, orgSettingsTemplate, nil
}

func (s *Server) putOrg(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, "", err
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		return nil, "", ErrInvalidRequestArg
	}
	org, err := s.Org(user, r)
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgSettingsRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		CanEdit:           org.UserID.Int32 == user.ID,
	}

	if !renderCtx.CanEdit {
		renderCtx.ErrorMessage = "Insufficient permissions to update settings."
		return renderCtx, orgSettingsTemplate, nil
	}

	name := strings.TrimSpace(r.FormValue(common.ParamName))
	if name != org.Name {
		if nameError := s.validateOrgName(ctx, name, user); len(nameError) > 0 {
			renderCtx.NameError = nameError
			return renderCtx, orgSettingsTemplate, nil
		}

		if updatedOrg, err := s.Store.Impl().UpdateOrganization(ctx, org, name); err != nil {
			renderCtx.ErrorMessage = "Failed to update settings. Please try again."
		} else {
			renderCtx.SuccessMessage = "Settings were updated"
			renderCtx.CurrentOrg = orgToUserOrg(updatedOrg, user.ID)
		}
	}

	return renderCtx, orgSettingsTemplate, nil
}
