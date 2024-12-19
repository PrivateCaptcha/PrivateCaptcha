package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/badoux/checkmail"
)

var (
	errInvalidSession = errors.New("session contains invalid data")
	maxOrgNameLength  = 255
	errNoOrgs         = errors.New("user has no organizations")
)

const (
	orgPropertiesTemplate = "portal/org-dashboard.html"
	orgSettingsTemplate   = "portal/org-settings.html"
	orgMembersTemplate    = "portal/org-members.html"
	orgWizardTemplate     = "org-wizard/wizard.html"
	portalTemplate        = "portal/portal.html"
)

type orgSettingsRenderContext struct {
	alertRenderContext
	csrfRenderContext
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
	alertRenderContext
	csrfRenderContext
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
	csrfRenderContext
	systemNotificationContext
	Orgs       []*userOrg
	CurrentOrg *userOrg
	// shortened from CurrentOrgProperties for simplicity
	Properties []*userProperty
}

type orgWizardRenderContext struct {
	csrfRenderContext
	alertRenderContext
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
			Level: org.Level,
		})
	}
	return result
}

func (s *Server) getNewOrg(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	return &orgWizardRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
	}, orgWizardTemplate, nil
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

func (s *Server) validateOrgsLimit(ctx context.Context, user *dbgen.User) string {
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
		return "You need an active subscription to create new organizations"
	}

	return ""
}

func (s *Server) postNewOrg(w http.ResponseWriter, r *http.Request) {
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

	renderCtx := &orgWizardRenderContext{
		csrfRenderContext:  s.createCsrfContext(user),
		alertRenderContext: alertRenderContext{},
	}

	name := r.FormValue(common.ParamName)
	if nameError := s.validateOrgName(ctx, name, user.ID); len(nameError) > 0 {
		renderCtx.NameError = nameError
		s.render(w, r, createOrgFormTemplate, renderCtx)
		return
	}

	if limitError := s.validateOrgsLimit(ctx, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
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

	user, err := s.sessionUser(ctx, sess)
	if err != nil {
		return nil, err
	}

	orgs, err := s.Store.RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user orgs", common.ErrAttr(err))
		return nil, err
	}

	if len(orgs) == 0 {
		slog.WarnContext(ctx, "User has no organizations")
		return nil, errNoOrgs
	}

	renderCtx := &orgDashboardRenderContext{
		csrfRenderContext:         s.createCsrfContext(user),
		systemNotificationContext: s.createSystemNotificationContext(ctx, sess),
		Orgs:                      orgsToUserOrgs(orgs),
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

// This cannot be "MVC" function since we're redirecting user to create new org if needed
func (s *Server) getPortal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)

	orgID, _, err := common.IntPathArg(r, common.ParamOrg)
	if err != nil {
		slog.WarnContext(ctx, "Org path argument is missing", common.ErrAttr(err))
		orgID = -1
	}

	renderCtx, err := s.createOrgDashboardContext(ctx, int32(orgID), sess)
	if err != nil {
		if (orgID == -1) && (err == errNoOrgs) {
			common.Redirect(s.partsURL(common.OrgEndpoint, common.NewEndpoint), w, r)
		} else if err == errInvalidSession {
			slog.WarnContext(ctx, "Inconsistent user session found")
			s.Session.SessionDestroy(w, r)
			common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		} else {
			s.redirectError(http.StatusInternalServerError, w, r)
		}
		return
	}

	s.render(w, r, portalTemplate, renderCtx)
}

func (s *Server) getOrgDashboard(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	org, err := s.org(r)
	if err != nil {
		return nil, "", err
	}

	properties, err := s.Store.RetrieveOrgProperties(ctx, org.ID)
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgPropertiesRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		Properties:        propertiesToUserProperties(ctx, properties),
	}

	return renderCtx, orgPropertiesTemplate, nil
}

func (s *Server) getOrgMembers(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	org, err := s.org(r)
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgMemberRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		CanEdit:           org.UserID.Int32 == user.ID,
	}

	if user.ID != org.UserID.Int32 {
		slog.WarnContext(ctx, "Fetching org members as not an owner", "userID", user.ID)
		return renderCtx, orgMembersTemplate, nil
	}

	members, err := s.Store.RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return nil, "", err
	}

	renderCtx.Members = usersToOrgUsers(members)

	return renderCtx, orgMembersTemplate, nil
}

func (s *Server) postOrgMembers(w http.ResponseWriter, r *http.Request) (Model, string, error) {
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

	org, err := s.org(r)
	if err != nil {
		return nil, "", err
	}

	members, err := s.Store.RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return nil, "", err
	}

	renderCtx := &orgMemberRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		Members:           usersToOrgUsers(members),
		CanEdit:           org.UserID.Int32 == user.ID,
	}

	if !renderCtx.CanEdit {
		renderCtx.ErrorMessage = "Only organization owner can invite other members."
		return renderCtx, orgMembersTemplate, nil
	}

	inviteEmail := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if inviteEmail == user.Email {
		renderCtx.ErrorMessage = "You are already a member of this organization."
		return renderCtx, orgMembersTemplate, nil
	}

	if err := checkmail.ValidateFormat(inviteEmail); err != nil {
		slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Email address is not valid."
		return renderCtx, orgMembersTemplate, nil
	}

	inviteUser, err := s.Store.FindUserByEmail(ctx, inviteEmail)
	if err != nil {
		renderCtx.ErrorMessage = fmt.Sprintf("Cannot find user account with email '%s'.", inviteEmail)
		return renderCtx, orgMembersTemplate, nil
	}

	if err = s.Store.InviteUserToOrg(ctx, org.UserID.Int32, inviteUser.ID); err != nil {
		renderCtx.ErrorMessage = "Failed to invite user. Please try again."
	} else {
		ou := userToOrgUser(inviteUser, string(dbgen.AccessLevelInvited))
		renderCtx.Members = append(renderCtx.Members, ou)
		renderCtx.SuccessMessage = "Invite is sent."
	}

	return renderCtx, orgMembersTemplate, nil
}

func (s *Server) deleteOrgMembers(w http.ResponseWriter, r *http.Request) {
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

	userID, value, err := common.IntPathArg(r, common.ParamUser)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse user from request", "value", value, common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	org, err := s.org(r)
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	if org.UserID.Int32 != user.ID {
		slog.ErrorContext(ctx, "Remove member request from not the org owner", "orgUserID", org.UserID.Int32, "userID", user.ID)
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	if err := s.Store.RemoveUserFromOrg(ctx, org.ID, int32(userID)); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) joinOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	orgID, err := s.orgID(r)
	if err != nil {
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	if err := s.Store.JoinOrg(ctx, orgID, user.ID); err == nil {
		// NOTE: we don't want to htmx-swap anything as we need to update the org dropdown
		common.Redirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(int(orgID))), w, r)
	} else {
		s.redirectError(http.StatusInternalServerError, w, r)
	}
}

func (s *Server) leaveOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	orgID, err := s.orgID(r)
	if err != nil {
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	if err := s.Store.LeaveOrg(ctx, int32(orgID), user.ID); err == nil {
		// NOTE: we don't want to htmx-swap anything as we need to update the org dropdown
		common.Redirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(int(orgID))), w, r)
	} else {
		s.redirectError(http.StatusInternalServerError, w, r)
	}
}

func (s *Server) getOrgSettings(w http.ResponseWriter, r *http.Request) (Model, string, error) {
	ctx := r.Context()
	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		return nil, "", err
	}

	org, err := s.org(r)
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgSettingsRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		CanEdit:           org.UserID.Int32 == user.ID,
	}

	return renderCtx, orgSettingsTemplate, nil
}

func (s *Server) putOrg(w http.ResponseWriter, r *http.Request) (Model, string, error) {
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
	org, err := s.org(r)
	if err != nil {
		return nil, "", err
	}

	renderCtx := &orgSettingsRenderContext{
		csrfRenderContext: s.createCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		CanEdit:           org.UserID.Int32 == user.ID,
	}

	if !renderCtx.CanEdit {
		renderCtx.ErrorMessage = "Insufficient permissions to update settings."
		return renderCtx, orgSettingsTemplate, nil
	}

	name := r.FormValue(common.ParamName)
	if name != org.Name {
		if nameError := s.validateOrgName(ctx, name, user.ID); len(nameError) > 0 {
			renderCtx.NameError = nameError
			return renderCtx, orgSettingsTemplate, nil
		}

		if updatedOrg, err := s.Store.UpdateOrganization(ctx, org.ID, name); err != nil {
			renderCtx.ErrorMessage = "Failed to update settings. Please try again."
		} else {
			renderCtx.SuccessMessage = "Settings were updated"
			renderCtx.CurrentOrg = orgToUserOrg(updatedOrg, user.ID)
		}
	}

	return renderCtx, orgSettingsTemplate, nil
}

func (s *Server) deleteOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.sessionUser(ctx, s.session(w, r))
	if err != nil {
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, err := s.org(r)
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	if org.UserID.Int32 != user.ID {
		slog.ErrorContext(ctx, "Does not have permissions to delete org", "userID", user.ID, "orgUserID", org.UserID.Int32)
		s.redirectError(http.StatusUnauthorized, w, r)
		return
	}

	if err := s.Store.SoftDeleteOrganization(ctx, org.ID, user.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to delete organization", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	common.Redirect(s.relURL("/"), w, r)
}
