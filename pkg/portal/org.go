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

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
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
	maxOrgFormSizeBytes   = 256 * 1024
)

type orgSettingsRenderContext struct {
	CurrentOrg    *userOrg
	Token         string
	NameError     string
	UpdateMessage string
	UpdateError   string
	CanEdit       bool
}

type orgUser struct {
	Name      string
	ID        string
	Level     string
	CreatedAt string
}

type orgMemberRenderContext struct {
	CurrentOrg    *userOrg
	Token         string
	InviteError   string
	InviteMessage string
	Members       []*orgUser
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

	r.Body = http.MaxBytesReader(w, r.Body, maxOrgFormSizeBytes)
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
		return nil, errInvalidSession
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
		} else if err == errInvalidSession {
			slog.WarnContext(ctx, "Inconsistent user session found")
			s.Session.SessionDestroy(w, r)
			common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		} else {
			s.redirectError(http.StatusInternalServerError, w, r)
		}
		return
	}

	s.render(w, r, "portal/portal.html", renderCtx)
}

func (s *Server) getOrgDashboard(w http.ResponseWriter, r *http.Request) {
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

	properties, err := s.Store.RetrieveOrgProperties(ctx, int32(orgID))
	if err != nil {
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &orgPropertiesRenderContext{
		CurrentOrg: orgToUserOrg(org, user.ID),
		Properties: propertiesToUserProperties(ctx, properties),
	}

	s.render(w, r, orgPropertiesTemplate, renderCtx)
}

func (s *Server) getOrgMembers(w http.ResponseWriter, r *http.Request) {
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

	renderCtx := &orgMemberRenderContext{
		CurrentOrg: orgToUserOrg(org, user.ID),
		CanEdit:    org.UserID.Int32 == user.ID,
	}

	if user.ID != org.UserID.Int32 {
		slog.WarnContext(ctx, "Fetching org members as not an owner", "userID", user.ID)
		s.render(w, r, orgMembersTemplate, renderCtx)
		return
	}

	members, err := s.Store.RetrieveOrganizationUsers(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx.Token = s.XSRF.Token(email, actionOrgMembers)
	renderCtx.Members = usersToOrgUsers(members)

	s.render(w, r, orgMembersTemplate, renderCtx)
}

func (s *Server) postOrgMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxOrgFormSizeBytes)
	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, email, actionOrgMembers) {
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

	members, err := s.Store.RetrieveOrganizationUsers(ctx, int32(orgID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &orgMemberRenderContext{
		CurrentOrg: orgToUserOrg(org, user.ID),
		Token:      s.XSRF.Token(email, actionOrgMembers),
		Members:    usersToOrgUsers(members),
		CanEdit:    org.UserID.Int32 == user.ID,
	}

	if !renderCtx.CanEdit {
		renderCtx.InviteError = "Only organization owner can invite other members."
		s.render(w, r, orgMembersTemplate, renderCtx)
		return
	}

	inviteEmail := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if err := checkmail.ValidateFormat(email); err != nil {
		slog.Warn("Failed to validate email format", common.ErrAttr(err))
		renderCtx.InviteError = "Email address is not valid."
		s.render(w, r, orgMembersTemplate, renderCtx)
		return
	}

	inviteUser, err := s.Store.FindUser(ctx, inviteEmail)
	if err != nil {
		renderCtx.InviteError = fmt.Sprintf("Cannot find user account with email '%s'.", inviteEmail)
		s.render(w, r, orgMembersTemplate, renderCtx)
		return
	}

	if err = s.Store.InviteUserToOrg(ctx, org.UserID.Int32, inviteUser.ID); err != nil {
		renderCtx.InviteError = "Failed to invite user. Please try again."
	} else {
		ou := userToOrgUser(inviteUser, string(dbgen.AccessLevelInvited))
		renderCtx.Members = append(renderCtx.Members, ou)
		renderCtx.InviteMessage = "Invite is sent."
	}

	s.render(w, r, orgMembersTemplate, renderCtx)
}

func (s *Server) deleteOrgMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxOrgFormSizeBytes)
	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	//token := r.FormValue(common.ParamCsrfToken)
	//if !s.XSRF.VerifyToken(token, email, actionOrgMembers) {
	//	slog.WarnContext(ctx, "Failed to verify CSRF token")
	//	common.Redirect(s.relURL(common.ExpiredEndpoint), w, r)
	//	return
	//}

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
		slog.ErrorContext(ctx, "Remove member request from not the org owner", "orgUserID", org.UserID.Int32, "userID", user.ID)
		http.Error(w, "", http.StatusUnauthorized)
		return
	}

	userID := ctx.Value(common.UserIDContextKey).(int)
	if err := s.Store.RemoveUserFromOrg(ctx, int32(orgID), int32(userID)); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) joinOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	orgID := ctx.Value(common.OrgIDContextKey).(int)

	if err := s.Store.JoinOrg(ctx, int32(orgID), user.ID); err == nil {
		// NOTE: we don't want to htmx-swap anything as we need to update the org dropdown
		common.Redirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(orgID)), w, r)
	} else {
		s.redirectError(http.StatusInternalServerError, w, r)
	}
}

func (s *Server) leaveOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)

	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.ErrorContext(ctx, "Failed to get email from session")
		common.Redirect(s.relURL(common.LoginEndpoint), w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.redirectError(http.StatusInternalServerError, w, r)
		return
	}

	orgID := ctx.Value(common.OrgIDContextKey).(int)

	if err := s.Store.LeaveOrg(ctx, int32(orgID), user.ID); err == nil {
		// NOTE: we don't want to htmx-swap anything as we need to update the org dropdown
		common.Redirect(s.partsURL(common.OrgEndpoint, strconv.Itoa(orgID)), w, r)
	} else {
		s.redirectError(http.StatusInternalServerError, w, r)
	}
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

	renderCtx := &orgSettingsRenderContext{
		CurrentOrg: orgToUserOrg(org, user.ID),
		Token:      s.XSRF.Token(email, actionOrgSettings),
		CanEdit:    org.UserID.Int32 == user.ID,
	}

	s.render(w, r, orgSettingsTemplate, renderCtx)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxOrgFormSizeBytes)
	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.redirectError(http.StatusBadRequest, w, r)
		return
	}

	token := r.FormValue(common.ParamCsrfToken)
	if !s.XSRF.VerifyToken(token, email, actionOrgSettings) {
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
		CurrentOrg: orgToUserOrg(org, user.ID),
		Token:      s.XSRF.Token(email, actionOrgSettings),
		CanEdit:    org.UserID.Int32 == user.ID,
	}

	if !renderCtx.CanEdit {
		renderCtx.UpdateError = "Insufficient permissions to update settings."
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
			renderCtx.CurrentOrg = orgToUserOrg(updatedOrg, user.ID)
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
