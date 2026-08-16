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

func (s *Server) moveForm(w http.ResponseWriter, r *http.Request) {
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

	org, level, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	if level.Valid && level.AccessLevel == dbgen.AccessLevelInvited {
		slog.WarnContext(ctx, "User is only invited, not a member of this org", "orgID", org.ID, "userID", user.ID)
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if org.ID == int32(newOrgID) {
		slog.ErrorContext(ctx, "Attempt to move form to the same org", "orgID", newOrgID)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	form, err := s.Form(org, r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	canMove := user.ID == form.CreatorID.Int32
	if !canMove {
		slog.ErrorContext(ctx, "Not enough permissions to move form", "userID", user.ID, "orgUserID", org.UserID.Int32, "formUserID", form.CreatorID.Int32)
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

	property, err := s.Store.Impl().RetrieveOrgProperty(ctx, org, form.PropertyID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve form property before move", "formID", form.ID, "propertyID", form.PropertyID, common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	var updatedForm *dbgen.Form
	auditEvents, err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		var moveAuditEvents []*common.AuditLogEvent
		var updatedProperty *dbgen.Property
		updatedForm, updatedProperty, moveAuditEvents, err = impl.MoveForm(ctx, user, form, property, orgs[idx])
		_ = updatedProperty
		return moveAuditEvents, err
	})
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	formDashboardURL := s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(updatedForm.OrgID.Int32)), common.FormEndpoint, s.IDHasher.Encrypt(int(updatedForm.ID)))
	common.Redirect(formDashboardURL, http.StatusOK, w, r)
	s.Store.AuditLog().RecordEvents(ctx, auditEvents, common.AuditLogSourcePortal)
}

func (s *Server) getFormAuditLogs(w http.ResponseWriter, r *http.Request) (*formAuditLogsRenderContext, *common.AuditLogEvent, error) {
	dashboardCtx, form, err := s.getOrgForm(w, r)
	if err != nil {
		return nil, nil, err
	}

	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, nil, err
	}

	renderCtx := &formAuditLogsRenderContext{
		formDashboardRenderContext: *dashboardCtx,
		AuditLogsRenderContext: AuditLogsRenderContext{
			AuditLogs: []*UserAuditLog{},
			SeeMore:   true,
		},
		CanView: (form.CreatorID.Int32 == user.ID) || (form.OrgOwnerID.Int32 == user.ID),
	}

	renderCtx.Tab = formAuditLogsTabIndex

	if !renderCtx.CanView {
		renderCtx.WarningMessage = "You do not have permissions to view audit logs of this form."
		return renderCtx, nil, nil
	}

	auditEvent := newAccessAuditLogEvent(user, db.TableNameForms, int64(form.ID), form.Name, common.AuditLogsEndpoint)

	const maxFormAuditLogs = 5
	logs, err := s.Store.Impl().RetrieveFormAuditLogs(ctx, form, maxFormAuditLogs)
	if err != nil {
		renderCtx.ErrorMessage = "Failed to retrieve form audit logs. Please try again later."
		return renderCtx, auditEvent, nil
	}
	renderCtx.AuditLogs = s.newFormAuditLogs(ctx, user, logs)
	renderCtx.PerPage = perPageEventLogs
	renderCtx.Count = len(renderCtx.AuditLogs)
	renderCtx.Page = 0

	return renderCtx, auditEvent, nil
}
