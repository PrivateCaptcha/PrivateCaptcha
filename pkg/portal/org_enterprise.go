//go:build enterprise

package portal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/badoux/checkmail"
)

const (
	errorMessageSelfAlreadyMember = "You are already a member of this organization."
	errorMessageUserAlreadyMember = "User with this email is already a member of this organization."
	errorMessageOrgMembersLimit   = "Organization members limit reached on your current plan, please upgrade to invite more."
	errorMessageOrgSubscription   = "You need an active subscription to invite organization members."
)

func (s *Server) validateOrgsLimit(ctx context.Context, user *dbgen.User) string {
	var subscr *dbgen.Subscription
	var err error

	if user.SubscriptionID.Valid {
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return ""
		}
	}

	if (subscr == nil) || !s.PlanService.IsSubscriptionActive(subscr.Status) {
		return activeSubscriptionForOrgError
	}

	isInternalSubscription := db.IsInternalSubscription(subscr.Source)
	plan, err := s.PlanService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, s.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return ""
	}

	// NOTE: this should be freshly cached as we should have just rendered the dashboard
	orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user orgs", "userID", user.ID, common.ErrAttr(err))
		return ""
	}

	if !plan.CheckOrgsLimit(len(orgs)) {
		slog.WarnContext(ctx, "Organizations limit check failed", "orgs", len(orgs), "userID", user.ID, "subscriptionID", subscr.ID,
			"plan", plan.Name(), "internal", isInternalSubscription)

		return "Organizations limit reached on your current plan, please upgrade to create more."
	}

	return ""
}

func (s *Server) postNewOrg(w http.ResponseWriter, r *http.Request) {
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

	renderCtx := &orgWizardRenderContext{
		CsrfRenderContext:  s.CreateCsrfContext(user),
		AlertRenderContext: AlertRenderContext{},
	}

	name := strings.TrimSpace(r.FormValue(common.ParamName))
	if nameError := s.validateOrgName(ctx, name, user); len(nameError) > 0 {
		renderCtx.NameError = nameError
		s.render(w, r, createOrgFormTemplate, renderCtx)
		return
	}

	if limitError := s.validateOrgsLimit(ctx, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
		s.render(w, r, createOrgFormTemplate, renderCtx)
		return
	}

	org, err := s.Store.Impl().CreateNewOrganization(ctx, name, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create organization", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	common.Redirect(s.PartsURL(common.OrgEndpoint, strconv.Itoa(int(org.ID))), http.StatusOK, w, r)
}

// here we know that user is already organization owner
func (s *Server) validateAddOrgMemberEmail(ctx context.Context, user *dbgen.User, members []*dbgen.GetOrganizationUsersRow, inviteEmail string) string {
	if inviteEmail == user.Email {
		return errorMessageSelfAlreadyMember
	}

	if err := checkmail.ValidateFormat(inviteEmail); err != nil {
		slog.WarnContext(ctx, "Failed to validate email format", common.ErrAttr(err))
		return "Email address is not valid."
	}

	existingIndex := slices.IndexFunc(members, func(r *dbgen.GetOrganizationUsersRow) bool { return r.User.Email == inviteEmail })
	if existingIndex != -1 {
		member := members[existingIndex]
		slog.WarnContext(ctx, "User is already a member", "userID", member.User.ID, "level", member.Level)
		return errorMessageUserAlreadyMember
	}

	var subscr *dbgen.Subscription
	var err error

	if user.SubscriptionID.Valid {
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return ""
		}
	}

	if (subscr == nil) || !s.PlanService.IsSubscriptionActive(subscr.Status) {
		return errorMessageOrgSubscription
	}

	isInternalSubscription := db.IsInternalSubscription(subscr.Source)
	plan, err := s.PlanService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, s.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return ""
	}

	if !plan.CheckOrgMembersLimit(len(members)) {
		return errorMessageOrgMembersLimit
	}

	return ""
}

// here we know that user is already organization owner
func (s *Server) validateAddOrgMemberID(ctx context.Context, user *dbgen.User, members []*dbgen.GetOrganizationUsersRow, inviteUserID int32) string {
	if inviteUserID == user.ID {
		return errorMessageSelfAlreadyMember
	}

	existingIndex := slices.IndexFunc(members, func(r *dbgen.GetOrganizationUsersRow) bool { return r.User.ID == inviteUserID })
	if existingIndex != -1 {
		member := members[existingIndex]
		slog.WarnContext(ctx, "User is already a member", "userID", member.User.ID, "level", member.Level)
		return errorMessageUserAlreadyMember
	}

	var subscr *dbgen.Subscription
	var err error

	if user.SubscriptionID.Valid {
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return ""
		}
	}

	if (subscr == nil) || !s.PlanService.IsSubscriptionActive(subscr.Status) {
		return errorMessageOrgSubscription
	}

	isInternalSubscription := db.IsInternalSubscription(subscr.Source)
	plan, err := s.PlanService.FindPlan(subscr.ExternalProductID, subscr.ExternalPriceID, s.Stage, isInternalSubscription)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find billing plan for subscription", "subscriptionID", subscr.ID, common.ErrAttr(err))
		return ""
	}

	if !plan.CheckOrgMembersLimit(len(members)) {
		return errorMessageOrgMembersLimit
	}

	return ""
}

func (s *Server) postOrgMembers(w http.ResponseWriter, r *http.Request) (Model, string, error) {
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

	members, err := s.Store.Impl().RetrieveOrganizationUsers(ctx, org)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return nil, "", err
	}

	renderCtx := &orgMemberRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		CurrentOrg:        orgToUserOrg(org, user.ID),
		Members:           usersToOrgUsers(members),
		CanEdit:           org.UserID.Int32 == user.ID,
	}

	if !renderCtx.CanEdit {
		renderCtx.ErrorMessage = "Only organization owner can invite other members."
		return renderCtx, orgMembersTemplate, nil
	}

	inviteEmail := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if errorMsg := s.validateAddOrgMemberEmail(ctx, user, members, inviteEmail); len(errorMsg) > 0 {
		renderCtx.ErrorMessage = errorMsg
		return renderCtx, orgMembersTemplate, nil
	}

	inviteUser, err := s.Store.Impl().FindUserByEmail(ctx, inviteEmail)
	if err != nil {
		renderCtx.ErrorMessage = fmt.Sprintf("Cannot find user account with email '%s'.", inviteEmail)
		return renderCtx, orgMembersTemplate, nil
	}

	if errorMsg := s.validateAddOrgMemberID(ctx, user, members, inviteUser.ID); len(errorMsg) > 0 {
		renderCtx.ErrorMessage = errorMsg
		return renderCtx, orgMembersTemplate, nil
	}

	if err = s.Store.Impl().InviteUserToOrg(ctx, org, inviteUser); err != nil {
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

	userID, value, err := common.IntPathArg(r, common.ParamUser)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse user from request", "value", value, common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	org, err := s.Org(user, r)
	if err != nil {
		code := http.StatusInternalServerError
		if err == db.ErrPermissions {
			code = http.StatusForbidden
		}

		s.RedirectError(code, w, r)
		return
	}

	if org.UserID.Int32 != user.ID {
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	if err := s.Store.Impl().RemoveUserFromOrg(ctx, org, int32(userID)); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) joinOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	orgID, err := s.OrgID(r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if err := s.Store.Impl().JoinOrg(ctx, orgID, user); err == nil {
		// NOTE: we don't want to htmx-swap anything as we need to update the org dropdown
		common.Redirect(s.PartsURL(common.OrgEndpoint, strconv.Itoa(int(orgID))), http.StatusOK, w, r)
	} else {
		s.RedirectError(http.StatusInternalServerError, w, r)
	}
}

func (s *Server) leaveOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	orgID, err := s.OrgID(r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if err := s.Store.Impl().LeaveOrg(ctx, orgID, user); err == nil {
		// NOTE: we don't want to htmx-swap anything as we need to update the org dropdown
		common.Redirect(s.PartsURL(common.OrgEndpoint, strconv.Itoa(int(orgID))), http.StatusOK, w, r)
	} else {
		s.RedirectError(http.StatusInternalServerError, w, r)
	}
}

func (s *Server) deleteOrg(w http.ResponseWriter, r *http.Request) {
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

	if org.UserID.Int32 != user.ID {
		slog.ErrorContext(ctx, "Does not have permissions to delete org", "userID", user.ID, "orgUserID", org.UserID.Int32)
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	if err := s.Store.Impl().SoftDeleteOrganization(ctx, org, user); err != nil {
		slog.ErrorContext(ctx, "Failed to delete organization", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	common.Redirect(s.RelURL("/"), http.StatusOK, w, r)
}
