//go:build enterprise

package portal

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func (s *Server) moveProperty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	newOrgParam := strings.TrimSpace(r.FormValue(common.ParamOrg))
	newOrgID, err := s.IDHasher.Decrypt(newOrgParam)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse new org ID", "value", newOrgParam, common.ErrAttr(err))
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

	if org.ID == int32(newOrgID) {
		// this shouldn't happen as we don't expose such option in FE
		slog.ErrorContext(ctx, "Attempt to move property to the same org", "orgID", newOrgID)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	property, err := s.Property(org, r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	// we can only move properties that we created
	canMove := user.ID == property.CreatorID.Int32
	if !canMove {
		slog.ErrorContext(ctx, "Not enough permissions to move property", "userID", user.ID,
			"orgUserID", org.UserID.Int32, "propertyUserID", property.CreatorID.Int32)
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	orgs, err := s.Store.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user orgs", common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	idx := slices.IndexFunc(orgs, func(o *dbgen.GetUserOrganizationsRow) bool {
		return (o.Organization.ID == int32(newOrgID)) && (o.Level == dbgen.AccessLevelOwner)
	})
	if idx == -1 {
		slog.ErrorContext(ctx, "Org is not found in user owned orgs", "orgID", newOrgID, "userID", user.ID)
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	// TODO: Show user success message after property is moved to new org
	// can put it in the session
	if updatedProperty, auditEvent, err := s.Store.Impl().MoveProperty(ctx, user, property, orgs[idx]); err == nil {
		propertyDashboardURL := s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(updatedProperty.OrgID.Int32)), common.PropertyEndpoint, s.IDHasher.Encrypt(int(updatedProperty.ID)))
		common.Redirect(propertyDashboardURL, http.StatusOK, w, r)
		s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)
	} else {
		s.RedirectError(http.StatusInternalServerError, w, r)
	}
}

func (s *Server) getPropertyAuditLogs(w http.ResponseWriter, r *http.Request) (*propertyAuditLogsRenderContext, *common.AuditLogEvent, error) {
	dashboardCtx, property, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, nil, err
	}

	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, nil, err
	}

	renderCtx := &propertyAuditLogsRenderContext{
		propertyDashboardRenderContext: *dashboardCtx,
		AuditLogsRenderContext: AuditLogsRenderContext{
			AuditLogs: []*UserAuditLog{},
			SeeMore:   true,
		},
		CanView: (property.CreatorID.Int32 == user.ID) || (property.OrgOwnerID.Int32 == user.ID),
	}

	renderCtx.Tab = propertyAuditLogsTabIndex

	if !renderCtx.CanView {
		renderCtx.WarningMessage = "You do not have permissions to view audit logs of this property."
		return renderCtx, nil, nil
	}

	auditEvent := newAccessAuditLogEvent(user, db.TableNameProperties, int64(property.ID), property.Name, common.AuditLogsEndpoint)

	const maxPropertyAuditLogs = 5
	logs, err := s.Store.Impl().RetrievePropertyAuditLogs(ctx, property, maxPropertyAuditLogs)
	if err != nil {
		renderCtx.ErrorMessage = "Failed to retrieve property audit logs. Please try again later."
		return renderCtx, auditEvent, nil
	}
	renderCtx.AuditLogs = s.newPropertyAuditLogs(ctx, user, logs)
	renderCtx.PerPage = perPageEventLogs
	renderCtx.Count = len(renderCtx.AuditLogs)
	renderCtx.Page = 0

	return renderCtx, auditEvent, nil
}

func (s *Server) getPropertyRules(w http.ResponseWriter, r *http.Request) (*propertyRulesRenderContext, *common.AuditLogEvent, error) {
	dashboardCtx, property, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, nil, err
	}

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

	renderCtx := &propertyRulesRenderContext{
		propertyDashboardRenderContext: *dashboardCtx,
		Rules:                          []*DifficultyRuleModel{},
	}

	renderCtx.Tab = propertyRulesTabIndex

	batch := map[int32]uint{property.ID: 1}
	rulesMap, err := s.Store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, batch)
	if err != nil {
		renderCtx.ErrorMessage = "Failed to retrieve difficulty rules. Please try again later."
		return renderCtx, nil, nil
	}

	rules := rulesMap[property.ID]
	for index, rule := range rules {
		canEdit := canEditRule(user, org, rule)
		renderCtx.Rules = append(renderCtx.Rules, difficultyRuleToDisplay(rule, index, canEdit, s.IDHasher))
	}

	return renderCtx, nil, nil
}
