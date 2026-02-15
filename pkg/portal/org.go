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

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

var (
	errInvalidSession = errors.New("session contains invalid data")
	errNoOrgs         = errors.New("user has no organizations")
	stubUserOrg       = &userOrg{ID: "-1"}
	propertiesPerPage = 30
)

const (
	orgDashboardTemplate          = "portal/org-dashboard.html"
	orgPropertiesTemplate         = "portal/properties.html"
	orgSettingsTemplate           = "portal/org-settings.html"
	orgMembersTemplate            = "portal/org-members.html"
	orgAuditLogsTemplate          = "portal/org-auditlogs.html"
	orgWizardTemplate             = "org-wizard/wizard.html"
	portalTemplate                = "portal/portal.html"
	activeSubscriptionForOrgError = "You need an active subscription to create new organizations."
	enterpriseOrgError            = "Creating new organizations is only available in the enterprise edition of Private Captcha."
	orgUserCreatedAtFormat        = "02 Jan 2006"
	portalMembersTabIndex         = 1
	portalSettingsTabIndex        = 2
	portalEventsTabIndex          = 3
)

type portalBaseRenderContext struct {
	CsrfRenderContext
	systemNotificationContext
	Orgs       []*userOrg
	CurrentOrg *userOrg
	Tab        int
}

type orgSettingsRenderContext struct {
	portalBaseRenderContext
	AlertRenderContext
	NameError   string
	Members     []*orgUser
	CanEdit     bool
	CanTransfer bool
}

type orgAuditLogsRenderContext struct {
	portalBaseRenderContext
	AlertRenderContext
	AuditLogsRenderContext
	CanView bool
}

type orgUser struct {
	Name      string
	Email     string
	ID        string
	Level     string
	CreatedAt string
}

type orgMemberRenderContext struct {
	portalBaseRenderContext
	AlertRenderContext
	Members []*orgUser
	CanEdit bool
}

type userOrg struct {
	Name  string
	ID    string
	Level string
}

type orgDashboardRenderContext struct {
	portalBaseRenderContext
	PaginationRenderContext
	// shortened from CurrentOrgProperties for simplicity
	Properties []*userProperty
}

type orgWizardRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	NameError string
}

func userToOrgUser(user *dbgen.User, level string, hasher common.IdentifierHasher) *orgUser {
	return &orgUser{
		Name:      user.Name,
		ID:        hasher.Encrypt(int(user.ID)),
		CreatedAt: user.CreatedAt.Time.Format(orgUserCreatedAtFormat),
		Level:     level,
	}
}

func usersToOrgUsers(users []*dbgen.GetOrganizationUsersRow, hasher common.IdentifierHasher) []*orgUser {
	result := make([]*orgUser, 0, len(users))

	for _, user := range users {
		result = append(result, userToOrgUser(&user.User, string(user.Level), hasher))
	}

	return result
}

func userWithEmailInviteToOrgUser(row *dbgen.GetOrganizationUsersWithEmailInvitesRow, hasher common.IdentifierHasher) *orgUser {
	ou := &orgUser{
		Level:     string(row.OrganizationUser.Level),
		CreatedAt: row.OrganizationUser.CreatedAt.Time.Format(orgUserCreatedAtFormat),
	}

	if row.LinkedUserID.Valid {
		// Linked user invite
		ou.ID = hasher.Encrypt(int(row.LinkedUserID.Int32))
		ou.Name = row.UserName.String
		ou.Email = common.MaskEmail(row.UserEmail.String, '*')
	} else if row.OrganizationUser.Email.Valid {
		// Email-only invite (not yet linked to a user)
		ou.ID = hasher.Encrypt(int(row.OrganizationUser.ID))
		ou.Email = common.MaskEmail(row.OrganizationUser.Email.String, '*')
	}

	return ou
}

func usersWithEmailInvitesToOrgUsers(users []*dbgen.GetOrganizationUsersWithEmailInvitesRow, hasher common.IdentifierHasher) []*orgUser {
	result := make([]*orgUser, 0, len(users))

	for _, row := range users {
		result = append(result, userWithEmailInviteToOrgUser(row, hasher))
	}

	return result
}

func orgToUserOrg(org *dbgen.Organization, userID int32, hasher common.IdentifierHasher) *userOrg {
	uo := &userOrg{
		Name: org.Name,
		ID:   hasher.Encrypt(int(org.ID)),
	}

	if org.UserID.Int32 == userID {
		uo.Level = string(dbgen.AccessLevelOwner)
	}

	return uo
}

func orgsToUserOrgs(orgs []*dbgen.GetUserOrganizationsRow, hasher common.IdentifierHasher) []*userOrg {
	result := make([]*userOrg, 0, len(orgs))
	for _, org := range orgs {
		result = append(result, &userOrg{
			Name:  org.Organization.Name,
			ID:    hasher.Encrypt(int(org.Organization.ID)),
			Level: string(org.Level),
		})
	}
	return result
}

func (s *Server) getNewOrg(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	renderCtx := &orgWizardRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
	}

	if !user.SubscriptionID.Valid {
		renderCtx.ErrorMessage = activeSubscriptionForOrgError
	} else if !s.isEnterprise() {
		renderCtx.WarningMessage = enterpriseOrgError
	}

	return &ViewModel{Model: renderCtx, View: orgWizardTemplate}, nil
}

func (s *Server) createPortalBaseContext(ctx context.Context, orgID int32, sess *session.Session) (*portalBaseRenderContext, error) {
	slog.DebugContext(ctx, "Creating portal base context", "orgID", orgID)

	user, err := s.SessionUser(ctx, sess)
	if err != nil {
		return nil, err
	}

	orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	if len(orgs) == 0 {
		slog.WarnContext(ctx, "User has no organizations")
		return nil, errNoOrgs
	}

	if !s.checkUserOrgsLimit(ctx, user, len(orgs)) {
		slog.WarnContext(ctx, "Organizations limit reached", "count", len(orgs))
		return nil, errLimitedFeature
	}

	idx := -1
	if orgID != -1 {
		idx = slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool { return o.Organization.ID == orgID })
		if idx == -1 {
			slog.WarnContext(ctx, "Org is not found in user orgs", "orgID", orgID, "userID", user.ID)
			return nil, errInvalidPathArg
		}
	}

	renderCtx := &portalBaseRenderContext{
		CsrfRenderContext:         s.CreateCsrfContext(user),
		systemNotificationContext: s.createSystemNotificationContext(ctx, sess),
		Orgs:                      orgsToUserOrgs(orgs, s.IDHasher),
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

		renderCtx.CurrentOrg = renderCtx.Orgs[earliestIdx]
		slog.DebugContext(ctx, "Selected current org as earliest owned", "index", earliestIdx)
	}

	return renderCtx, nil
}

func (s *Server) createOrgDashboardContext(ctx context.Context, orgID int32, sess *session.Session) (*orgDashboardRenderContext, error) {
	baseCtx, err := s.createPortalBaseContext(ctx, orgID, sess)
	if err != nil {
		return nil, err
	}

	renderCtx := &orgDashboardRenderContext{
		portalBaseRenderContext: *baseCtx,
		Properties:              []*userProperty{},
	}

	if baseCtx.CurrentOrg.Level == string(dbgen.AccessLevelInvited) {
		return renderCtx, nil
	}

	user, err := s.SessionUser(ctx, sess)
	if err != nil {
		return renderCtx, nil
	}

	decryptedOrgID, err := s.IDHasher.Decrypt(baseCtx.CurrentOrg.ID)
	if err != nil {
		return renderCtx, nil
	}

	org, _, err := s.Store.Impl().RetrieveUserOrganization(ctx, user, int32(decryptedOrgID))
	if err != nil {
		return renderCtx, nil
	}

	if properties, hasMore, err := s.Store.Impl().RetrieveOrgProperties(ctx, org, 0 /*offset*/, propertiesPerPage); err == nil {
		renderCtx.Properties = propertiesToUserProperties(ctx, properties, s.IDHasher)

		renderCtx.PaginationRenderContext = PaginationRenderContext{
			From:    1,
			To:      len(properties),
			Count:   len(properties),
			Page:    0,
			PerPage: propertiesPerPage,
		}

		if hasMore {
			if count, err := s.Store.Impl().RetrieveOrgPropertiesCount(ctx, org.ID); err == nil {
				renderCtx.Count = int(count)
			}
		}
	}

	return renderCtx, nil
}

// This cannot be "MVC" function since we're redirecting user to create new org if needed
func (s *Server) getPortal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session(w, r)

	orgID, _, err := common.IntPathArg(r, common.ParamOrg, s.IDHasher)
	if err != nil {
		slog.WarnContext(ctx, "Org path argument is missing", common.ErrAttr(err))
		orgID = -1
	}

	tabParam := r.URL.Query().Get(common.ParamTab)
	slog.Log(ctx, common.LevelTrace, "Portal tab was requested", "tab", tabParam)

	isDashboardTab := tabParam == "" || tabParam == common.DashboardEndpoint
	if isDashboardTab {
		renderCtx, err := s.createOrgDashboardContext(ctx, orgID, sess)
		if err != nil {
			s.handlePortalError(orgID, err, w, r)
			return
		}
		s.render(w, r, portalTemplate, renderCtx)
		return
	}

	baseCtx, err := s.createPortalBaseContext(ctx, orgID, sess)
	if err != nil {
		s.handlePortalError(orgID, err, w, r)
		return
	}

	if baseCtx.CurrentOrg.Level == string(dbgen.AccessLevelInvited) {
		s.render(w, r, portalTemplate, &orgDashboardRenderContext{
			portalBaseRenderContext: *baseCtx,
			Properties:              []*userProperty{},
		})
		return
	}

	var model Model
	var derr error
	switch tabParam {
	case common.MembersEndpoint:
		if vm, err := s.getOrgMembers(w, r); err == nil {
			if mc, ok := vm.Model.(*orgMemberRenderContext); ok {
				mc.Orgs = baseCtx.Orgs
				mc.systemNotificationContext = baseCtx.systemNotificationContext
			}
			model = vm.Model
		} else {
			derr = err
		}
	case common.SettingsEndpoint:
		if vm, err := s.getOrgSettings(w, r); err == nil {
			if sc, ok := vm.Model.(*orgSettingsRenderContext); ok {
				sc.Orgs = baseCtx.Orgs
				sc.systemNotificationContext = baseCtx.systemNotificationContext
			}
			model = vm.Model
		} else {
			derr = err
		}
	case common.EventsEndpoint:
		if vm, err := s.getOrgAuditLogs(w, r); err == nil {
			if ac, ok := vm.Model.(*orgAuditLogsRenderContext); ok {
				ac.Orgs = baseCtx.Orgs
				ac.systemNotificationContext = baseCtx.systemNotificationContext
			}
			model = vm.Model
		} else {
			derr = err
		}
	default:
		slog.ErrorContext(ctx, "Unknown tab requested", "tab", tabParam)
		renderCtx, err := s.createOrgDashboardContext(ctx, orgID, sess)
		if err != nil {
			s.handlePortalError(orgID, err, w, r)
			return
		}
		s.render(w, r, portalTemplate, renderCtx)
		return
	}

	if derr != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	s.render(w, r, portalTemplate, model)
}

func (s *Server) handlePortalError(orgID int32, err error, w http.ResponseWriter, r *http.Request) {
	if (orgID == -1) && (err == errNoOrgs) {
		common.Redirect(s.PartsURL(common.OrgEndpoint, common.NewEndpoint), http.StatusOK, w, r)
	} else if err == errInvalidSession {
		slog.WarnContext(r.Context(), "Inconsistent user session found")
		s.Sessions.SessionDestroy(w, r)
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
	} else if err == errInvalidPathArg {
		s.RedirectError(http.StatusBadRequest, w, r)
	} else if err == errLimitedFeature {
		s.RedirectError(http.StatusPaymentRequired, w, r)
	} else {
		s.RedirectError(http.StatusInternalServerError, w, r)
	}
}

func (s *Server) createOrgPropertiesContext(ctx context.Context, org *dbgen.Organization, user *dbgen.User, page int) (*orgPropertiesRenderContext, error) {
	if page < 0 {
		page = 0
	}

	properties, hasMore, err := s.Store.Impl().RetrieveOrgProperties(ctx, org, page*propertiesPerPage, propertiesPerPage)
	if err != nil {
		return nil, err
	}

	from := 1 + page*propertiesPerPage

	renderCtx := &orgPropertiesRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		PaginationRenderContext: PaginationRenderContext{
			From:    from,
			To:      from + len(properties) - 1,
			Count:   len(properties),
			Page:    page,
			PerPage: propertiesPerPage,
		},
		CurrentOrg: orgToUserOrg(org, user.ID, s.IDHasher),
		Properties: propertiesToUserProperties(ctx, properties, s.IDHasher),
	}

	if (page > 0) || hasMore {
		if count, err := s.Store.Impl().RetrieveOrgPropertiesCount(ctx, org.ID); err == nil {
			renderCtx.Count = int(count)
		}
	}

	return renderCtx, nil
}

func (s *Server) getOrgDashboard(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	renderCtx, err := s.createOrgPropertiesContext(ctx, org, user, 0 /*page*/)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: orgDashboardTemplate}, nil
}

func (s *Server) getOrgProperties(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	pageParam := r.URL.Query().Get(common.ParamPage)
	page := 0
	if len(pageParam) > 0 {
		if page, err = strconv.Atoi(pageParam); err != nil {
			slog.ErrorContext(ctx, "Failed to convert page parameter", "page", pageParam, common.ErrAttr(err))
			page = 0
		}
	}

	renderCtx, err := s.createOrgPropertiesContext(ctx, org, user, page)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: orgPropertiesTemplate}, nil
}

func (s *Server) getOrgMembers(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	renderCtx := &orgMemberRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{
			CsrfRenderContext: s.CreateCsrfContext(user),
			CurrentOrg:        orgToUserOrg(org, user.ID, s.IDHasher),
			Tab:               portalMembersTabIndex,
		},
		CanEdit: org.UserID.Int32 == user.ID,
	}

	if user.ID != org.UserID.Int32 {
		slog.WarnContext(ctx, "Fetching org members as not an owner", "userID", user.ID)
		return &ViewModel{Model: renderCtx, View: orgMembersTemplate}, nil
	}

	members, err := s.Store.Impl().RetrieveOrganizationUsersWithEmailInvites(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return nil, err
	}

	renderCtx.Members = usersWithEmailInvitesToOrgUsers(members, s.IDHasher)

	return &ViewModel{
		Model:      renderCtx,
		View:       orgMembersTemplate,
		AuditEvent: newAccessAuditLogEvent(user, db.TableNameOrgs, int64(org.ID), org.Name, common.MembersEndpoint),
	}, nil
}

func (s *Server) getOrgSettings(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	renderCtx := &orgSettingsRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{
			CsrfRenderContext: s.CreateCsrfContext(user),
			CurrentOrg:        orgToUserOrg(org, user.ID, s.IDHasher),
			Tab:               portalSettingsTabIndex,
		},
		CanEdit:     org.UserID.Int32 == user.ID,
		CanTransfer: false,
		Members:     []*orgUser{},
	}

	// Fetch org members for transfer dropdown (only for owners and enterprise)
	if renderCtx.CanEdit && s.isEnterprise() {
		if members, err := s.Store.Impl().RetrieveOrganizationUsers(ctx, org.ID); err == nil {
			// Filter to only include accepted members (not pending invites)
			acceptedMembers := make([]*orgUser, 0, len(members))
			for _, m := range members {
				if m.Level == dbgen.AccessLevelMember {
					acceptedMembers = append(acceptedMembers, userToOrgUser(&m.User, string(m.Level), s.IDHasher))
				}
			}
			renderCtx.Members = acceptedMembers
			renderCtx.CanTransfer = len(acceptedMembers) > 0
		}
	}

	return &ViewModel{
		Model:      renderCtx,
		View:       orgSettingsTemplate,
		AuditEvent: newAccessAuditLogEvent(user, db.TableNameOrgs, int64(org.ID), org.Name, common.SettingsEndpoint),
	}, nil
}

func (s *Server) newOrganizationAuditLogs(ctx context.Context, user *dbgen.User, logs []*dbgen.GetOrgAuditLogsRow) []*UserAuditLog {
	result := make([]*UserAuditLog, 0, len(logs))

	for _, log := range logs {
		if ul, err := s.newUserAuditLog(ctx, &log.AuditLog); err == nil {
			if log.Name.Valid && log.Email.Valid {
				ul.UserName = log.Name.String
				ul.UserEmail = common.MaskEmail(log.Email.String, '*')
			} else {
				ul.UserName = "Unknown User"
				ul.UserEmail = "-"
			}

			result = append(result, ul)
		}
	}

	return result
}

func (s *Server) getOrgAuditLogs(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	renderCtx, auditEvent, err := s.createOrgAuditLogsContext(ctx, org, user)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model:      renderCtx,
		View:       orgAuditLogsTemplate,
		AuditEvent: auditEvent,
	}, nil
}

func (s *Server) putOrg(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
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

	renderCtx := &orgSettingsRenderContext{
		portalBaseRenderContext: portalBaseRenderContext{
			CsrfRenderContext: s.CreateCsrfContext(user),
			CurrentOrg:        orgToUserOrg(org, user.ID, s.IDHasher),
			Tab:               portalSettingsTabIndex,
		},
		CanEdit: org.UserID.Int32 == user.ID,
	}

	if !renderCtx.CanEdit {
		renderCtx.ErrorMessage = "Insufficient permissions to update settings."
		return &ViewModel{Model: renderCtx, View: orgSettingsTemplate}, nil
	}

	var auditEvent *common.AuditLogEvent
	name := strings.TrimSpace(r.FormValue(common.ParamName))
	if name != org.Name {
		if nameStatus := s.Store.Impl().ValidateOrgName(ctx, name, user); !nameStatus.Success() {
			renderCtx.NameError = nameStatus.String()
			return &ViewModel{Model: renderCtx, View: orgSettingsTemplate}, nil
		}

		var updatedOrg *dbgen.Organization
		if updatedOrg, auditEvent, err = s.Store.Impl().UpdateOrganization(ctx, user, org, name); err != nil {
			renderCtx.ErrorMessage = "Failed to update settings. Please try again."
		} else {
			renderCtx.SuccessMessage = "Settings were updated"
			renderCtx.CurrentOrg = orgToUserOrg(updatedOrg, user.ID, s.IDHasher)
		}
	}

	return &ViewModel{Model: renderCtx, View: orgSettingsTemplate, AuditEvent: auditEvent}, nil
}
