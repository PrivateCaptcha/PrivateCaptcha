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
	ErrInvalidSession = errors.New("session contains invalid data")
	errNoOrgs         = errors.New("user has no organizations")
	stubUserOrg       = &UserOrg{ID: "-1"}
	propertiesPerPage = 30
)

const (
	orgDashboardTemplate          = "portal/org-dashboard.html"
	orgFormsListTemplate          = "portal/forms.html"
	orgFormsTemplate              = "portal/org-forms.html"
	orgPropertiesTemplate         = "portal/properties.html"
	orgSettingsTemplate           = "portal/org-settings.html"
	orgMembersTemplate            = "portal/org-members.html"
	orgAuditLogsTemplate          = "portal/org-auditlogs.html"
	orgRulesTemplate              = "portal/org-rules.html"
	orgWizardTemplate             = "org-wizard/wizard.html"
	portalTemplate                = "portal/portal.html"
	activeSubscriptionForOrgError = "You need an active subscription to create new organizations."
	enterpriseOrgError            = "Creating new organizations is only available in the enterprise edition of Private Captcha."
	orgUserCreatedAtFormat        = "02 Jan 2006"
	portalPropertiesTabIndex      = 0
	portalFormsTabIndex           = 1
	portalMembersTabIndex         = 2
	portalSettingsTabIndex        = 3
	portalRulesTabIndex           = 4
	portalEventsTabIndex          = 5
)

type portalBaseRenderContext struct {
	CsrfRenderContext
	SystemNotificationContext
	Orgs           []*UserOrg
	CurrentOrg     *UserOrg
	Tab            int
	CanEdit        bool
	ShowOnboarding bool
}

type orgSettingsRenderContext struct {
	portalBaseRenderContext
	AlertRenderContext
	NameError   string
	Members     []*orgUser
	CanTransfer bool
}

type orgAuditLogsRenderContext struct {
	portalBaseRenderContext
	AlertRenderContext
	AuditLogsRenderContext
	CanView bool
}

type OrgRulesRenderContext struct {
	portalBaseRenderContext
	AlertRenderContext
	rulesRenderContext
	// Property is a stub to distinguish org rules from property rules in shared templates
	Property interface{}
}

type orgUser struct {
	Name          string
	Email         string
	ID            string
	Level         string
	CreatedAt     string
	IsEmailInvite bool
}

type orgMemberRenderContext struct {
	portalBaseRenderContext
	AlertRenderContext
	Members []*orgUser
}

type UserOrg struct {
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
		ou.IsEmailInvite = true
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

func orgToUserOrg(org *dbgen.Organization, userID int32, hasher common.IdentifierHasher) *UserOrg {
	uo := &UserOrg{
		Name: org.Name,
		ID:   hasher.Encrypt(int(org.ID)),
	}

	if org.UserID.Int32 == userID {
		uo.Level = string(dbgen.AccessLevelOwner)
	}

	return uo
}

func orgsToUserOrgs(orgs []*dbgen.GetUserOrganizationsRow, hasher common.IdentifierHasher) []*UserOrg {
	result := make([]*UserOrg, 0, len(orgs))
	for _, org := range orgs {
		result = append(result, &UserOrg{
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

	return &ViewModel{Model: renderCtx, View: orgWizardTemplate, IsNew: true}, nil
}

func (s *Server) createPortalTabBaseContext(org *dbgen.Organization, user *dbgen.User, tab int) *portalBaseRenderContext {
	return &portalBaseRenderContext{
		CsrfRenderContext:         s.CreateCsrfContext(user),
		SystemNotificationContext: SystemNotificationContext{},
		CurrentOrg:                orgToUserOrg(org, user.ID, s.IDHasher),
		Tab:                       tab,
		CanEdit:                   org.UserID.Int32 == user.ID,
	}
}

func (s *Server) createPortalBaseContext(ctx context.Context, orgID int32, sess *session.Session, tab int) (*portalBaseRenderContext, *dbgen.Organization, error) {
	slog.DebugContext(ctx, "Creating portal base context", "orgID", orgID, "tab", tab)

	user, err := s.SessionUser(ctx, sess)
	if err != nil {
		return nil, nil, err
	}

	orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	if len(orgs) == 0 {
		slog.WarnContext(ctx, "User has no organizations")
		return nil, nil, errNoOrgs
	}

	if !s.checkUserOrgsLimit(ctx, user, len(orgs)) {
		slog.WarnContext(ctx, "Organizations limit reached", "count", len(orgs))
		return nil, nil, errLimitedFeature
	}

	idx := -1
	if orgID != -1 {
		idx = slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool { return o.Organization.ID == orgID })
		if idx == -1 {
			slog.WarnContext(ctx, "Org is not found in user orgs", "orgID", orgID, "userID", user.ID)
			return nil, nil, ErrInvalidPathArg
		}
	}

	renderCtx := &portalBaseRenderContext{
		CsrfRenderContext:         s.CreateCsrfContext(user),
		SystemNotificationContext: s.createSystemNotificationContext(ctx, sess),
		Orgs:                      orgsToUserOrgs(orgs, s.IDHasher),
		CurrentOrg:                stubUserOrg,
		Tab:                       tab,
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
		slog.DebugContext(ctx, "Selected current org as earliest owned", "index", earliestIdx)
	}

	org := &orgs[idx].Organization
	renderCtx.CanEdit = (org.UserID.Int32 == user.ID)

	return renderCtx, org, nil
}

func (s *Server) createOrgDashboardContext(ctx context.Context, baseCtx *portalBaseRenderContext, org *dbgen.Organization) (*orgDashboardRenderContext, error) {
	baseCtx.Tab = portalPropertiesTabIndex

	renderCtx := &orgDashboardRenderContext{
		portalBaseRenderContext: *baseCtx,
		Properties:              []*userProperty{},
	}

	if baseCtx.CurrentOrg.Level == string(dbgen.AccessLevelInvited) {
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

func (s *Server) createOrgFormsRenderContext(ctx context.Context, baseCtx *portalBaseRenderContext, org *dbgen.Organization, page int) (*orgFormsRenderContext, error) {
	baseCtx.Tab = portalFormsTabIndex
	if page < 0 {
		page = 0
	}

	renderCtx := &orgFormsRenderContext{
		portalBaseRenderContext: *baseCtx,
		Forms:                   []*userForm{},
	}

	if baseCtx.CurrentOrg.Level == string(dbgen.AccessLevelInvited) {
		return renderCtx, nil
	}

	forms, hasMore, err := s.Store.Impl().RetrieveOrgForms(ctx, org, page*propertiesPerPage, propertiesPerPage)
	if err != nil {
		return nil, err
	}

	renderCtx.Forms = formsToUserForms(ctx, forms, s.IDHasher)
	renderCtx.PaginationRenderContext = PaginationRenderContext{
		Count:   len(renderCtx.Forms),
		Page:    page,
		PerPage: propertiesPerPage,
	}

	if len(renderCtx.Forms) > 0 {
		from := 1 + page*propertiesPerPage
		renderCtx.From = from
		renderCtx.To = from + len(renderCtx.Forms) - 1
	}

	if (page > 0) || hasMore {
		if count, err := s.Store.Impl().RetrieveOrgFormsCount(ctx, org.ID); err == nil {
			renderCtx.Count = int(count)
		}
	}

	return renderCtx, nil
}

func (s *Server) handlePortalError(orgID int32, err error, w http.ResponseWriter, r *http.Request) {
	if (orgID == -1) && (err == errNoOrgs) {
		common.Redirect(s.PartsURL(common.OrgEndpoint, common.NewEndpoint), http.StatusOK, w, r)
	} else if err == ErrInvalidSession {
		slog.WarnContext(r.Context(), "Inconsistent user session found")
		s.Sessions.SessionDestroy(w, r)
		common.Redirect(s.RelURL(common.LoginEndpoint), http.StatusUnauthorized, w, r)
	} else if err == ErrInvalidPathArg {
		s.RedirectError(http.StatusBadRequest, w, r)
	} else if err == errLimitedFeature {
		s.RedirectError(http.StatusPaymentRequired, w, r)
	} else {
		s.RedirectError(http.StatusInternalServerError, w, r)
	}
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

	var baseCtx *portalBaseRenderContext
	var org *dbgen.Organization

	if tabParam != common.RulesEndpoint {
		baseCtx, org, err = s.createPortalBaseContext(ctx, orgID, sess, portalPropertiesTabIndex)
		if err != nil {
			s.handlePortalError(orgID, err, w, r)
			return
		}

		if baseCtx.CurrentOrg.Level == string(dbgen.AccessLevelInvited) {
			s.render(w, r, portalTemplate, &orgDashboardRenderContext{
				portalBaseRenderContext: *baseCtx,
				Properties:              []*userProperty{},
			}, true /*new*/)
			return
		}
	}

	user, err := s.SessionUser(ctx, sess)
	if err != nil {
		s.handlePortalError(orgID, err, w, r)
		return
	}

	var model Model
	var derr error
	var event *common.AuditLogEvent
	switch tabParam {
	case common.FormsEndpoint:
		if vm, err := s.createOrgFormsRenderContext(ctx, baseCtx, org, 0 /*page*/); err == nil {
			model = vm
		} else {
			derr = err
		}
	case common.MembersEndpoint:
		if vm, ae, err := s.createOrgMembersRenderContext(ctx, baseCtx, org, user); err == nil {
			model = vm
			event = ae
		} else {
			derr = err
		}
	case common.SettingsEndpoint:
		if vm, ae, err := s.createOrgSettingsRenderContext(ctx, baseCtx, org, user); err == nil {
			model = vm
			event = ae
		} else {
			derr = err
		}
	case common.EventsEndpoint:
		if vm, ae, err := s.createOrgAuditLogsContext(ctx, baseCtx, org, user); err == nil {
			model = vm
			event = ae
		} else {
			derr = err
		}
	case common.RulesEndpoint:
		if vm, ae, err := s.OrgRulesFunc(w, r); err == nil {
			model = vm
			event = ae
		} else {
			derr = err
		}
	default:
		if (tabParam != "") && (tabParam != common.DashboardEndpoint) {
			slog.ErrorContext(ctx, "Unknown tab requested", "tab", tabParam)
		}
		if vm, err := s.createOrgDashboardContext(ctx, baseCtx, org); err == nil {
			if _, ok := sess.Get(ctx, session.KeyFirstSession).(bool); ok {
				onboardingParam := r.URL.Query().Get(common.ParamOnboarding)
				vm.ShowOnboarding = common.ParseBoolean(onboardingParam)
			}
			model = vm
		} else {
			derr = err
		}
	}

	if derr != nil {
		s.handlePortalError(orgID, derr, w, r)
		return
	}

	if event != nil {
		s.Store.AuditLog().RecordEvent(ctx, event, common.AuditLogSourcePortal)
	}

	s.render(w, r, portalTemplate, model, true /*new*/)
}

func (s *Server) createOrgPropertiesContext(ctx context.Context, org *dbgen.Organization, user *dbgen.User, page int) (*orgPropertiesRenderContext, error) {
	if page < 0 {
		page = 0
	}

	properties, hasMore, err := s.Store.Impl().RetrieveOrgProperties(ctx, org, page*propertiesPerPage, propertiesPerPage)
	if err != nil {
		return nil, err
	}

	renderCtx := &orgPropertiesRenderContext{
		PaginationRenderContext: PaginationRenderContext{

			Count:   len(properties),
			Page:    page,
			PerPage: propertiesPerPage,
		},
		CurrentOrg: orgToUserOrg(org, user.ID, s.IDHasher),
		Properties: propertiesToUserProperties(ctx, properties, s.IDHasher),
	}

	if len(properties) > 0 {
		from := 1 + page*propertiesPerPage
		renderCtx.From = from
		renderCtx.To = from + len(properties) - 1
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

	org, level, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		return nil, db.ErrPermissions
	}

	renderCtx, err := s.createOrgPropertiesContext(ctx, org, user, 0 /*page*/)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: orgDashboardTemplate, IsNew: true}, nil
}

func (s *Server) getOrgFormsTab(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		return nil, db.ErrPermissions
	}

	baseCtx := s.createPortalTabBaseContext(org, user, portalFormsTabIndex)
	renderCtx, err := s.createOrgFormsRenderContext(ctx, baseCtx, org, 0 /*page*/)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: orgFormsTemplate, IsNew: true}, nil
}

func (s *Server) getOrgForms(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		return nil, db.ErrPermissions
	}

	pageParam := r.URL.Query().Get(common.ParamPage)
	page := 0
	if len(pageParam) > 0 {
		if page, err = strconv.Atoi(pageParam); err != nil {
			slog.ErrorContext(ctx, "Failed to convert page parameter", "page", pageParam, common.ErrAttr(err))
			page = 0
		}
	}

	baseCtx := s.createPortalTabBaseContext(org, user, portalFormsTabIndex)
	renderCtx, err := s.createOrgFormsRenderContext(ctx, baseCtx, org, page)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: orgFormsListTemplate, IsNew: false}, nil
}

func (s *Server) getOrgProperties(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		return nil, db.ErrPermissions
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

	return &ViewModel{Model: renderCtx, View: orgPropertiesTemplate, IsNew: false}, nil
}

func (s *Server) createOrgMembersRenderContext(ctx context.Context, baseCtx *portalBaseRenderContext, org *dbgen.Organization, user *dbgen.User) (*orgMemberRenderContext, *common.AuditLogEvent, error) {
	baseCtx.Tab = portalMembersTabIndex

	renderCtx := &orgMemberRenderContext{
		portalBaseRenderContext: *baseCtx,
	}

	event := newAccessAuditLogEvent(user, db.TableNameOrgs, int64(org.ID), org.Name, common.MembersEndpoint)

	if !baseCtx.CanEdit {
		slog.WarnContext(ctx, "Fetching org members as not an owner")
		return renderCtx, event, nil
	}

	members, err := s.Store.Impl().RetrieveOrganizationUsersWithEmailInvites(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org users", common.ErrAttr(err))
		return nil, event, err
	}

	renderCtx.Members = usersWithEmailInvitesToOrgUsers(members, s.IDHasher)

	return renderCtx, event, nil
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

	baseCtx := s.createPortalTabBaseContext(org, user, portalMembersTabIndex)
	renderCtx, event, err := s.createOrgMembersRenderContext(ctx, baseCtx, org, user)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model:       renderCtx,
		View:        orgMembersTemplate,
		AuditEvents: singleAuditEvents(event),
		IsNew:       true,
	}, nil
}

func (s *Server) createOrgSettingsRenderContext(ctx context.Context, baseCtx *portalBaseRenderContext, org *dbgen.Organization, user *dbgen.User) (*orgSettingsRenderContext, *common.AuditLogEvent, error) {
	baseCtx.Tab = portalSettingsTabIndex

	renderCtx := &orgSettingsRenderContext{
		portalBaseRenderContext: *baseCtx,
		CanTransfer:             false,
		Members:                 []*orgUser{},
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

	event := newAccessAuditLogEvent(user, db.TableNameOrgs, int64(org.ID), org.Name, common.SettingsEndpoint)

	return renderCtx, event, nil
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

	baseCtx := s.createPortalTabBaseContext(org, user, portalSettingsTabIndex)
	renderCtx, event, err := s.createOrgSettingsRenderContext(ctx, baseCtx, org, user)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model:       renderCtx,
		View:        orgSettingsTemplate,
		AuditEvents: singleAuditEvents(event),
		IsNew:       true,
	}, nil
}

func (s *Server) newOrganizationAuditLogs(ctx context.Context, user *dbgen.User, logs []*dbgen.GetOrgAuditLogsRow) []*UserAuditLog {
	result := make([]*UserAuditLog, 0, len(logs))

	for _, log := range logs {
		if ul, err := s.NewUserAuditLog(ctx, &log.AuditLog); err == nil {
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

	baseCtx := s.createPortalTabBaseContext(org, user, portalEventsTabIndex)
	renderCtx, auditEvent, err := s.createOrgAuditLogsContext(ctx, baseCtx, org, user)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model:       renderCtx,
		View:        orgAuditLogsTemplate,
		AuditEvents: singleAuditEvents(auditEvent),
		IsNew:       true,
	}, nil
}

func (s *Server) CreateOrgRulesContext(w http.ResponseWriter, r *http.Request) (*OrgRulesRenderContext, *common.AuditLogEvent, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, nil, err
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		return nil, nil, err
	}

	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		slog.WarnContext(ctx, "User is only invited, not a member of this org", "orgID", org.ID, "userID", user.ID)
		return nil, nil, db.ErrPermissions
	}

	baseCtx := s.createPortalTabBaseContext(org, user, portalRulesTabIndex)
	return s.createOrgRulesCtx(ctx, baseCtx, org, user)
}

func (s *Server) getOrgRules(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	renderCtx, auditEvent, err := s.OrgRulesFunc(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model:       renderCtx,
		View:        orgRulesTemplate,
		AuditEvents: singleAuditEvents(auditEvent),
		IsNew:       true,
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

	baseCtx := s.createPortalTabBaseContext(org, user, portalSettingsTabIndex)
	renderCtx, _, err := s.createOrgSettingsRenderContext(ctx, baseCtx, org, user)
	if err != nil {
		return nil, err
	}

	if !renderCtx.CanEdit {
		renderCtx.ErrorMessage = "Insufficient permissions to update settings."
		return &ViewModel{Model: renderCtx, View: orgSettingsTemplate, IsNew: false}, nil
	}

	var auditEvent *common.AuditLogEvent
	name := strings.TrimSpace(r.FormValue(common.ParamName))
	if name != org.Name {
		if nameStatus := s.Store.Impl().ValidateOrgName(ctx, name, user); !nameStatus.Success() {
			renderCtx.NameError = nameStatus.String()
			return &ViewModel{Model: renderCtx, View: orgSettingsTemplate, IsNew: false}, nil
		}

		var updatedOrg *dbgen.Organization
		if updatedOrg, auditEvent, err = s.Store.Impl().UpdateOrganization(ctx, user, org, name); err != nil {
			renderCtx.ErrorMessage = "Failed to update settings. Please try again."
		} else {
			renderCtx.SuccessMessage = "Settings were updated"
			renderCtx.CurrentOrg = orgToUserOrg(updatedOrg, user.ID, s.IDHasher)
		}
	}

	return &ViewModel{Model: renderCtx, View: orgSettingsTemplate, AuditEvents: singleAuditEvents(auditEvent), IsNew: false}, nil
}
