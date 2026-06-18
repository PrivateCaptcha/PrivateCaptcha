//go:build enterprise

package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
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
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32, false /*skip cache*/)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return ""
		}
	}

	ok, extra, err := s.SubscriptionLimits.CheckOrgsLimit(ctx, user.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return activeSubscriptionForOrgError
		}
		return ""
	}

	if !ok {
		slog.WarnContext(ctx, "Organizations limit check failed", "extra", extra, "userID", user.ID, "subscriptionID", subscr.ID,
			"internal", db.IsInternalSubscription(subscr.Source))

		return common.StatusOrgLimitError.String()
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
	if nameStatus := s.Store.Impl().ValidateOrgName(ctx, name, user); !nameStatus.Success() {
		renderCtx.NameError = nameStatus.String()
		s.render(w, r, createOrgFormTemplate, renderCtx, false /*new*/)
		return
	}

	if limitError := s.validateOrgsLimit(ctx, user); len(limitError) > 0 {
		renderCtx.ErrorMessage = limitError
		s.render(w, r, createOrgFormTemplate, renderCtx, false /*new*/)
		return
	}

	org, auditEvent, err := s.Store.Impl().CreateNewOrganization(ctx, name, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create the organization", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to create the organization. Please try again later."
		s.render(w, r, createOrgFormTemplate, renderCtx, false /*new*/)
		return
	}

	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID))), http.StatusOK, w, r)

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)
}

// here we know that user is already organization owner
func (s *Server) validateAddOrgMemberEmail(ctx context.Context, user *dbgen.User, org *dbgen.Organization, members []*dbgen.GetOrganizationUsersRow, inviteEmail string) string {
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
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32, false /*skip cache*/)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return ""
		}
	}

	ok, extra, err := s.SubscriptionLimits.CheckOrgMembersLimit(ctx, org.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return errorMessageOrgSubscription
		}
		return ""
	}

	if !ok {
		slog.WarnContext(ctx, "Organization members limit check failed", "extra", extra, "orgID", org.ID, "subscriptionID", subscr.ID,
			"internal", db.IsInternalSubscription(subscr.Source))
		return errorMessageOrgMembersLimit
	}

	return ""
}

// here we know that user is already organization owner
func (s *Server) validateAddOrgMemberID(ctx context.Context, user *dbgen.User, org *dbgen.Organization, members []*dbgen.GetOrganizationUsersRow, inviteUserID int32) string {
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
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32, false /*skip cache*/)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return ""
		}
	}

	ok, extra, err := s.SubscriptionLimits.CheckOrgMembersLimit(ctx, org.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return errorMessageOrgSubscription
		}
		return ""
	}

	if !ok {
		slog.WarnContext(ctx, "Organization members limit check failed", "extra", extra, "orgID", org.ID, "subscriptionID", subscr.ID,
			"internal", db.IsInternalSubscription(subscr.Source))
		return errorMessageOrgMembersLimit
	}

	return ""
}

func (s *Server) postOrgMembers(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
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

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	members, err := s.Store.Impl().RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return nil, err
	}

	renderCtx := &orgMemberRenderContext{
		portalBaseRenderContext: *s.createPortalTabBaseContext(org, user, portalMembersTabIndex),
	}

	if !renderCtx.CanEdit {
		renderCtx.ErrorMessage = "Only organization owner can invite other members."
		return &ViewModel{Model: renderCtx, View: orgMembersTemplate, IsNew: false}, nil
	}

	inviteEmail := strings.TrimSpace(r.FormValue(common.ParamEmail))
	if errorMsg := s.validateAddOrgMemberEmail(ctx, user, org, members, inviteEmail); len(errorMsg) > 0 {
		renderCtx.ErrorMessage = errorMsg
		return &ViewModel{Model: renderCtx, View: orgMembersTemplate, IsNew: false}, nil
	}

	inviteUser, err := s.Store.Impl().FindUserByEmail(ctx, inviteEmail)
	if err != nil {
		// User not found - check if we have a valid email and invite by email
		if err == db.ErrRecordNotFound {
			// Verify email format
			if err := s.EmailVerifier.VerifyEmail(ctx, inviteEmail); err != nil {
				slog.WarnContext(ctx, "Failed to validate email format for invite", common.ErrAttr(err))
				renderCtx.ErrorMessage = "Email address is not valid."
				return &ViewModel{Model: renderCtx, View: orgMembersTemplate, IsNew: false}, nil
			}

			// Invite by email
			return s.inviteEmailToOrg(ctx, user, org, inviteEmail, renderCtx)
		}
		if errors.Is(err, db.ErrDisabled) || errors.Is(err, db.ErrSoftDeleted) {
			slog.WarnContext(ctx, "Cannot invite unavailable user to org", "email", inviteEmail, common.ErrAttr(err))
			renderCtx.ErrorMessage = "Cannot invite this user to the organization."
			return &ViewModel{Model: renderCtx, View: orgMembersTemplate, IsNew: false}, nil
		}
		slog.ErrorContext(ctx, "Error finding user by email", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to invite user. Please try again."
		return &ViewModel{Model: renderCtx, View: orgMembersTemplate, IsNew: false}, nil
	}

	if errorMsg := s.validateAddOrgMemberID(ctx, user, org, members, inviteUser.ID); len(errorMsg) > 0 {
		renderCtx.ErrorMessage = errorMsg
		return &ViewModel{Model: renderCtx, View: orgMembersTemplate, IsNew: false}, nil
	}

	var auditEvent *common.AuditLogEvent
	if auditEvent, err = s.Store.Impl().InviteUserToOrg(ctx, user, org, inviteUser); err != nil {
		renderCtx.ErrorMessage = "Failed to invite user. Please try again."
	} else {
		ou := userToOrgUser(inviteUser, string(dbgen.AccessLevelInvited), s.IDHasher)
		renderCtx.Members = append(renderCtx.Members, ou)
		renderCtx.SuccessMessage = "Invite is sent."

		go common.RunAdHocFunc(common.CopyTraceID(ctx, context.Background()), func(bctx context.Context) error {
			orgURLPath := s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)))
			return s.Mailer.SendOrgInvite(bctx, inviteUser.Email, common.GuessFirstName(inviteUser.Name, inviteUser.Email),
				org.Name, user.Email, common.GuessFirstName(user.Name, user.Email), orgURLPath, false /* register*/)
		})
	}

	return &ViewModel{Model: renderCtx, View: orgMembersTemplate, AuditEvents: singleAuditEvents(auditEvent), IsNew: false}, nil
}

// inviteEmailToOrg handles inviting a non-existing user by email
func (s *Server) inviteEmailToOrg(ctx context.Context, user *dbgen.User, org *dbgen.Organization, email string, renderCtx *orgMemberRenderContext) (*ViewModel, error) {
	inviteRecord, auditEvent, err := s.Store.Impl().InviteEmailToOrg(ctx, user, org, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create email invite", common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to invite user. Please try again."
		return &ViewModel{Model: renderCtx, View: orgMembersTemplate, IsNew: false}, nil
	}

	// Add pending invite to the list (with email only, no user info)
	ou := &orgUser{
		ID:    s.IDHasher.Encrypt(int(inviteRecord.ID)),
		Email: common.MaskEmail(email, '*'),
		Level: string(dbgen.AccessLevelInvited),
	}
	renderCtx.Members = append(renderCtx.Members, ou)
	renderCtx.SuccessMessage = "Invite is sent."

	// Send invite email with registration link
	go common.RunAdHocFunc(common.CopyTraceID(ctx, context.Background()), func(bctx context.Context) error {
		registerInviteURL := s.PartsURL(common.OrgInviteEndpoint, s.IDHasher.Encrypt(int(inviteRecord.ID)), common.RegisterEndpoint)
		return s.Mailer.SendOrgInvite(bctx, email, "" /*user name*/, org.Name, user.Email, common.GuessFirstName(user.Name, user.Email), registerInviteURL, true /*register*/)
	})

	return &ViewModel{Model: renderCtx, View: orgMembersTemplate, AuditEvents: singleAuditEvents(auditEvent), IsNew: false}, nil
}

func (s *Server) deleteOrgMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	userID, value, err := common.IntPathArg(r, common.ParamUser, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse user from request", "value", value, common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	org, _, err := s.Org(user, r)
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

	if auditEvent, err := s.Store.Impl().RemoveUserFromOrg(ctx, user, org, int32(userID)); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	} else {
		s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)
	}

	slog.InfoContext(ctx, "Removed org member", "userID", userID, "orgID", org.ID)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) deleteOrgInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	inviteID, value, err := common.IntPathArg(r, common.ParamID, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse invite from request", "value", value, common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	org, _, err := s.Org(user, r)
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

	if auditEvent, err := s.Store.Impl().RemoveEmailInviteFromOrg(ctx, user, org, int32(inviteID)); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	} else {
		s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)
	}

	slog.InfoContext(ctx, "Removed org email invite", "inviteID", inviteID, "orgID", org.ID)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) joinOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if !level.Valid || level.AccessLevel != dbgen.AccessLevelInvited {
		slog.WarnContext(ctx, "User not invited to this org", "orgID", org.ID, "userID", user.ID)
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	_, ownerSubscr, err := s.Store.Impl().RetrieveOrgOwnerWithSubscription(ctx, org, user, false /*skip cache*/)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org owner subscription", "orgID", org.ID, common.ErrAttr(err))
		// NOTE: we intentionally allow this to happen as a safe fallback
	}

	if ownerSubscr != nil {
		if ok, extra, err := s.SubscriptionLimits.CheckOrgMembersLimit(ctx, org.ID, ownerSubscr); err == nil && !ok {
			slog.WarnContext(ctx, "Organization members limit check failed", "extra", extra, "orgID", org.ID, "subscriptionID", ownerSubscr.ID,
				"internal", db.IsInternalSubscription(ownerSubscr.Source))
			s.RedirectError(http.StatusPaymentRequired, w, r)
			return
		}
	}

	if auditEvent, err := s.Store.Impl().JoinOrg(ctx, org.ID, user); err == nil {
		// NOTE: we don't want to htmx-swap anything as we need to update the org dropdown
		common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID))), http.StatusOK, w, r)
		s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)
	} else {
		s.RedirectError(http.StatusInternalServerError, w, r)
	}

	// if user has no subscription, but joins the org, owned by a subscriber, user can access org resources
	go common.RunAdHocFunc(common.CopyTraceID(ctx, context.Background()), func(bctx context.Context) error {
		// purely theoretically it could have been better to first check if they have a subscription etc. etc.
		// but there's already a background pipeline for that, so...
		s.UserLimiter.DropUser(ctx, user.ID)
		return nil
	})
}

func (s *Server) leaveOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if !level.Valid || level.AccessLevel != dbgen.AccessLevelMember {
		slog.WarnContext(ctx, "User not a member of this org", "orgID", org.ID, "userID", user.ID)
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	if auditEvent, err := s.Store.Impl().LeaveOrg(ctx, org.ID, user); err == nil {
		// NOTE: we don't want to htmx-swap anything as we need to update the org dropdown
		common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID))), http.StatusOK, w, r)
		s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)
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

	org, _, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	if org.UserID.Int32 != user.ID {
		slog.ErrorContext(ctx, "Does not have permissions to delete org", "userID", user.ID, "orgUserID", org.UserID.Int32)
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	if auditEvent, err := s.Store.Impl().SoftDeleteOrganization(ctx, org, user); err != nil {
		slog.ErrorContext(ctx, "Failed to delete organization", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	} else {
		s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)
	}

	common.Redirect(s.RelURL("/"), http.StatusOK, w, r)
}

func (s *Server) validateTransferOrgLimits(ctx context.Context, org *dbgen.Organization, newOwner *dbgen.User) int {
	var subscr *dbgen.Subscription
	var err error

	if newOwner.SubscriptionID.Valid {
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, newOwner.SubscriptionID.Int32, false /*skip cache*/)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve destination user subscription", "userID", newOwner.ID, common.ErrAttr(err))
			return http.StatusInternalServerError
		}
	}

	propertiesLimit, err := s.SubscriptionLimits.PropertiesLimit(ctx, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return http.StatusPaymentRequired
		}

		slog.ErrorContext(ctx, "Failed to retrieve destination user property limit", "userID", newOwner.ID, common.ErrAttr(err))
		return http.StatusInternalServerError
	}

	if propertiesLimit > 0 {
		ok, extra, err := s.SubscriptionLimits.CheckPropertiesLimit(ctx, newOwner.ID, subscr)
		if err != nil {
			if err == db.ErrNoActiveSubscription {
				return http.StatusPaymentRequired
			}

			slog.ErrorContext(ctx, "Failed to check destination user property limit", "userID", newOwner.ID, common.ErrAttr(err))
			return http.StatusInternalServerError
		}

		if !ok && extra > 0 {
			slog.WarnContext(ctx, "Destination user is already above properties limit", "userID", newOwner.ID, "orgID", org.ID, "extra", extra)
			return http.StatusPaymentRequired
		}

		orgPropertiesCount, err := s.Store.Impl().RetrieveOrgPropertiesCount(ctx, org.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve organization properties count", "orgID", org.ID, common.ErrAttr(err))
			return http.StatusInternalServerError
		}

		// When extra < 0, user is under limit; -extra is remaining capacity
		if int(orgPropertiesCount) > (-extra) {
			slog.WarnContext(ctx, "Destination user would exceed properties limit after org transfer", "userID", newOwner.ID,
				"orgID", org.ID, "orgPropertiesCount", orgPropertiesCount, "extra", extra)
			return http.StatusPaymentRequired
		}
	}

	formsLimit, err := s.SubscriptionLimits.FormsLimit(ctx, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return http.StatusPaymentRequired
		}
		slog.ErrorContext(ctx, "Failed to retrieve destination user forms limit", "userID", newOwner.ID, common.ErrAttr(err))
		return http.StatusInternalServerError
	}

	if formsLimit > 0 {
		ok, extra, err := s.SubscriptionLimits.CheckFormsLimit(ctx, newOwner.ID, subscr)
		if err != nil {
			if err == db.ErrNoActiveSubscription {
				return http.StatusPaymentRequired
			}
			slog.ErrorContext(ctx, "Failed to check destination user forms limit", "userID", newOwner.ID, "orgID", org.ID, common.ErrAttr(err))
			return http.StatusInternalServerError
		}

		if !ok && extra > 0 {
			slog.WarnContext(ctx, "Destination user is already above forms limit", "userID", newOwner.ID, "orgID", org.ID, "extra", extra)
			return http.StatusPaymentRequired
		}

		orgFormsCount, err := s.Store.Impl().RetrieveOrgFormsCount(ctx, org.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve organization forms count", "orgID", org.ID, common.ErrAttr(err))
			return http.StatusInternalServerError
		}

		if int(orgFormsCount) > (-extra) {
			slog.WarnContext(ctx, "Destination user would exceed forms limit after org transfer", "userID", newOwner.ID,
				"orgID", org.ID, "orgFormsCount", orgFormsCount, "extra", extra)
			return http.StatusPaymentRequired
		}
	}

	return 0
}

func (s *Server) transferOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	newOwnerParam := strings.TrimSpace(r.FormValue(common.ParamUser))
	newOwnerID, err := s.IDHasher.Decrypt(newOwnerParam)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse new owner ID", "value", newOwnerParam, common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	// Only the current owner can transfer the organization
	if org.UserID.Int32 != user.ID {
		slog.ErrorContext(ctx, "Not enough permissions to transfer org", "userID", user.ID, "orgUserID", org.UserID.Int32)
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	// Verify that the new owner is a member of this organization (not just invited)
	members, err := s.Store.Impl().RetrieveOrganizationUsers(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org members", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	idx := slices.IndexFunc(members, func(m *dbgen.GetOrganizationUsersRow) bool {
		return (m.User.ID == int32(newOwnerID)) && (m.Level == dbgen.AccessLevelMember)
	})
	if idx == -1 {
		slog.ErrorContext(ctx, "New owner is not an accepted member of the org", "newOwnerID", newOwnerID, "orgID", org.ID)
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	newOwner := &members[idx].User
	if code := s.validateTransferOrgLimits(ctx, org, newOwner); code > 0 {
		s.RedirectError(code, w, r)
		return
	}

	auditEvents, err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		return impl.TransferOrganization(ctx, user, org, newOwner)
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to transfer organization", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	s.Store.AuditLog().RecordEvents(ctx, auditEvents, common.AuditLogSourcePortal)

	// Redirect to portal root - existing handlers will take care of the rest
	common.Redirect(s.RelURL("/"), http.StatusOK, w, r)
}

func (s *Server) createOrgAuditLogsContext(ctx context.Context, baseCtx *portalBaseRenderContext, org *dbgen.Organization, user *dbgen.User) (*orgAuditLogsRenderContext, *common.AuditLogEvent, error) {
	baseCtx.Tab = portalEventsTabIndex

	renderCtx := &orgAuditLogsRenderContext{
		portalBaseRenderContext: *baseCtx,
		AuditLogsRenderContext: AuditLogsRenderContext{
			AuditLogs: []*UserAuditLog{},
			SeeMore:   true,
		},
		CanView: baseCtx.CanEdit,
	}

	const maxOrgAuditLogs = 10

	var auditEvent *common.AuditLogEvent

	if renderCtx.CanView {
		auditEvent = newAccessAuditLogEvent(user, db.TableNameOrgs, int64(org.ID), org.Name, common.AuditLogsEndpoint)

		if logs, err := s.Store.Impl().RetrieveOrganizationAuditLogs(ctx, org, maxOrgAuditLogs); err == nil {
			renderCtx.AuditLogs = s.newOrganizationAuditLogs(ctx, user, logs)
			renderCtx.PerPage = perPageEventLogs
			renderCtx.Count = len(renderCtx.AuditLogs)
			renderCtx.Page = 0
		} else {
			renderCtx.ErrorMessage = "Failed to retrieve organization audit logs. Please try again later."
		}
	} else {
		renderCtx.WarningMessage = "You do not have permissions to view audit logs of this organization."
	}

	return renderCtx, auditEvent, nil
}

func (s *Server) createOrgRulesCtx(ctx context.Context, baseCtx *portalBaseRenderContext, org *dbgen.Organization, user *dbgen.User) (*OrgRulesRenderContext, *common.AuditLogEvent, error) {
	renderCtx := &OrgRulesRenderContext{
		portalBaseRenderContext: *baseCtx,
		rulesRenderContext: rulesRenderContext{
			Rules:     []*DifficultyRuleModel{},
			CanAddNew: baseCtx.CanEdit,
		},
	}
	renderCtx.Tab = portalRulesTabIndex

	batch := map[int32]uint{org.ID: 1}
	rulesMap, err := s.Store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, batch)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org difficulty rules", "orgID", org.ID, common.ErrAttr(err))
		renderCtx.ErrorMessage = "Failed to retrieve difficulty rules. Please try again later."
		return renderCtx, nil, nil
	}

	rules := rulesMap[org.ID]
	for _, rule := range rules {
		canEdit := canEditRule(user, org, rule)
		renderCtx.Rules = append(renderCtx.Rules, DifficultyRuleToDisplay(rule, canEdit, s.IDHasher, s.Rules))
	}

	return renderCtx, nil, nil
}

// getOrgInviteRegister handles the register-invite URL for users invited by email
func (s *Server) getOrgInviteRegister(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	if !s.canRegister.Load() {
		return nil, errRegistrationDisabled
	}

	// Validate invite ID from URL
	inviteIDStr := r.PathValue(common.ParamID)
	inviteID, err := s.IDHasher.Decrypt(inviteIDStr)
	if err != nil || inviteID <= 0 {
		slog.WarnContext(ctx, "Invalid invite ID in URL", "idStr", inviteIDStr, common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}

	model := &loginRenderContext{
		CsrfRenderContext: CsrfRenderContext{
			Token: s.XSRF.Token(""),
		},
		CaptchaRenderContext: s.CreateCaptchaRenderContext(db.PortalRegisterSitekey),
		IsRegister:           true,
	}

	// For security, try cached lookup first. If not found, still render register page
	// The actual invite validation will happen after 2FA in the background job
	if invite, err := s.Store.Impl().GetCachedOrgInviteByID(ctx, int32(inviteID)); err == nil {
		if invite.UserID.Valid {
			// Invite already linked to a user
			slog.InfoContext(ctx, "Invite already linked to a user", "inviteID", inviteID, "userID", invite.UserID.Int32)
			model.CaptchaRenderContext = s.CreateCaptchaRenderContext(db.PortalLoginSitekey)
			model.IsRegister = false
			model.CanRegister = s.canRegister.Load()
			return &ViewModel{Model: model, View: loginTemplate, IsNew: true}, nil
		}

		model.Email = invite.Email.String

		// Store invite ID in session so we can link it after registration
		sess := s.Sessions.SessionStart(w, r)
		_ = sess.Set(ctx, session.KeyOrgInviteID, int32(inviteID))
		_ = sess.Set(ctx, session.KeyPersistent, true)
	}

	// Return the register page view (same as regular register)
	return &ViewModel{Model: model, View: loginTemplate, IsNew: true}, nil
}
