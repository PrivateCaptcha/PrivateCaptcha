//go:build enterprise

package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func orgToAPIOrg(org *dbgen.Organization, hasher common.IdentifierHasher) *apiOrgOutput {
	return &apiOrgOutput{
		Name: org.Name,
		ID:   hasher.Encrypt(int(org.ID)),
	}
}

func orgsToAPIOrgs(orgs []*dbgen.GetUserOrganizationsRow, hasher common.IdentifierHasher, onlyOwned bool, orgID *int32) []*apiOrgOutput {
	result := make([]*apiOrgOutput, 0, len(orgs))
	for _, org := range orgs {
		if (!onlyOwned || (org.Level == dbgen.AccessLevelOwner)) &&
			((orgID == nil) || (org.Organization.ID == *orgID)) {
			result = append(result, &apiOrgOutput{
				Name: org.Organization.Name,
				ID:   hasher.Encrypt(int(org.Organization.ID)),
			})
		}
	}
	return result
}

// the difference from portal server is that we are more strict here
func (s *Server) validateOrgsLimit(ctx context.Context, user *dbgen.User) (bool, error) {
	var subscr *dbgen.Subscription
	var err error

	if user.SubscriptionID.Valid {
		subscr, err = s.BusinessDB.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve user subscription", "userID", user.ID, common.ErrAttr(err))
			return false, err
		}
	}

	ok, extra, err := s.SubscriptionLimits.CheckOrgsLimit(ctx, user.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return false, nil
		}
		return false, err
	}

	if !ok {
		slog.WarnContext(ctx, "Organizations limit check failed", "extra", extra, "userID", user.ID, "subscriptionID", subscr.ID,
			"internal", db.IsInternalSubscription(subscr.Source))

		return false, nil
	}

	return true, nil
}

func (s *Server) getUserOrgs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, true /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	orgs, err := s.BusinessDB.Impl().RetrieveUserOrganizations(ctx, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve user organizations", common.ErrAttr(err))
		s.sendAPIErrorResponse(ctx, common.StatusFailure, r, w)
		return
	}

	var orgID *int32
	if apiKey.OrgID.Valid {
		orgID = &apiKey.OrgID.Int32
	}

	orgsOutput := orgsToAPIOrgs(orgs, s.IDHasher, true /*only owned*/, orgID)
	s.sendAPISuccessResponseEx(ctx, &APIResponse{Data: orgsOutput}, w, common.NoCacheHeaders)
}

func (s *Server) postNewOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, false /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	if r.Header.Get(common.HeaderContentType) != common.ContentTypeJSON {
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	if apiKey.OrgID.Valid {
		slog.WarnContext(ctx, "API key is scoped to the organization", "orgID", apiKey.OrgID.Int32)
		s.sendHTTPErrorResponse(db.ErrPermissions, w)
		return
	}

	request := &apiOrgInput{}
	if err := json.NewDecoder(r.Body).Decode(request); err != nil {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to deserialize post org request", common.ErrAttr(err))
		}
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	if len(request.ID) > 0 {
		slog.ErrorContext(ctx, "Request org ID is not empty")
		s.sendAPIErrorResponse(ctx, common.StatusOrgIDNotEmptyError, r, w)
		return
	}

	if nameStatus := s.BusinessDB.Impl().ValidateOrgName(ctx, request.Name, user); !nameStatus.Success() {
		s.sendAPIErrorResponse(ctx, nameStatus, r, w)
		return
	}

	if ok, err := s.validateOrgsLimit(ctx, user); !ok || err != nil {
		s.sendAPIErrorResponse(ctx, common.StatusOrgLimitError, r, w)
		return
	}

	org, auditEvent, err := s.BusinessDB.Impl().CreateNewOrganization(ctx, request.Name, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create the organization", common.ErrAttr(err))
		s.sendAPIErrorResponse(ctx, common.StatusFailure, r, w)
		return
	}

	ao := orgToAPIOrg(org, s.IDHasher)
	s.sendAPISuccessResponse(ctx, ao, w)

	s.BusinessDB.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourceAPI)
}

func (s *Server) updateOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, false /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	if r.Header.Get(common.HeaderContentType) != common.ContentTypeJSON {
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	request := &apiOrgInput{}
	if err := json.NewDecoder(r.Body).Decode(request); err != nil {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to deserialize update org request", common.ErrAttr(err))
		}
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	if len(request.ID) == 0 {
		slog.WarnContext(ctx, "Empty org ID")
		s.sendAPIErrorResponse(ctx, common.StatusOrgIDEmptyError, r, w)
		return
	}

	if len(request.Name) == 0 {
		slog.WarnContext(ctx, "Empty org name")
		s.sendAPIErrorResponse(ctx, common.StatusOrgNameEmptyError, r, w)
		return
	}

	orgID, err := s.IDHasher.Decrypt(request.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decrypt org ID", "value", request.ID, common.ErrAttr(err))
		s.sendAPIErrorResponse(ctx, common.StatusOrgIDInvalidError, r, w)
		return
	}

	if apiKey.OrgID.Valid && (int32(orgID) != apiKey.OrgID.Int32) {
		slog.WarnContext(ctx, "API key org scope mismatch", "actualOrgID", orgID, "apiOrgID", apiKey.OrgID.Int32)
		s.sendHTTPErrorResponse(db.ErrPermissions, w)
		return
	}

	if nameStatus := s.BusinessDB.Impl().ValidateOrgName(ctx, request.Name, user); !nameStatus.Success() {
		s.sendAPIErrorResponse(ctx, nameStatus, r, w)
		return
	}

	oldOrg, err := s.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, int32(orgID))
	if err != nil {
		switch err {
		case db.ErrPermissions:
			s.sendAPIErrorResponse(ctx, common.StatusOrgPermissionsError, r, w)
		case db.ErrSoftDeleted:
			s.sendAPIErrorResponse(ctx, common.StatusOrgNotFoundError, r, w)
		default:
			s.sendAPIErrorResponse(ctx, common.StatusFailure, r, w)
		}
		return
	}

	if !oldOrg.UserID.Valid || (oldOrg.UserID.Int32 != user.ID) {
		slog.ErrorContext(ctx, "Attempt to update a non-owned org", "orgID", orgID, "userID", user.ID, "ownerID", oldOrg.UserID.Int32)
		s.sendAPIErrorResponse(ctx, common.StatusOrgPermissionsError, r, w)
		return
	}

	newOrg, auditEvent, err := s.BusinessDB.Impl().UpdateOrganization(ctx, user, oldOrg, request.Name)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update the organization", common.ErrAttr(err))
		s.sendAPIErrorResponse(ctx, common.StatusFailure, r, w)
		return
	}

	ao := orgToAPIOrg(newOrg, s.IDHasher)
	s.sendAPISuccessResponse(ctx, ao, w)

	s.BusinessDB.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourceAPI)
}

func (s *Server) deleteOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, apiKey, err := s.requestUser(ctx, false /*read-only*/)
	if err != nil {
		s.sendHTTPErrorResponse(err, w)
		return
	}

	if r.Header.Get(common.HeaderContentType) != common.ContentTypeJSON {
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	request := &apiOrgInput{}
	if err := json.NewDecoder(r.Body).Decode(request); err != nil {
		if err != io.EOF {
			slog.WarnContext(ctx, "Failed to deserialize delete org request", common.ErrAttr(err))
		}
		s.sendHTTPErrorResponse(db.ErrInvalidInput, w)
		return
	}

	if len(request.ID) == 0 {
		slog.ErrorContext(ctx, "Org ID is empty for delete request")
		s.sendAPIErrorResponse(ctx, common.StatusOrgIDEmptyError, r, w)
		return
	}

	orgID, err := s.IDHasher.Decrypt(request.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decrypt org ID", "value", request.ID, common.ErrAttr(err))
		s.sendAPIErrorResponse(ctx, common.StatusOrgIDInvalidError, r, w)
		return
	}

	if apiKey.OrgID.Valid && (int32(orgID) != apiKey.OrgID.Int32) {
		slog.WarnContext(ctx, "API key org scope mismatch", "actualOrgID", orgID, "apiOrgID", apiKey.OrgID.Int32)
		s.sendHTTPErrorResponse(db.ErrPermissions, w)
		return
	}

	org, err := s.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, int32(orgID))
	if err != nil {
		switch err {
		case db.ErrPermissions:
			s.sendAPIErrorResponse(ctx, common.StatusOrgPermissionsError, r, w)
		case db.ErrSoftDeleted:
			s.sendAPIErrorResponse(ctx, common.StatusOrgNotFoundError, r, w)
		default:
			s.sendAPIErrorResponse(ctx, common.StatusFailure, r, w)
		}
		return
	}

	if !org.UserID.Valid || (org.UserID.Int32 != user.ID) {
		slog.ErrorContext(ctx, "Attempt to delete a non-owned org", "orgID", orgID, "userID", user.ID, "ownerID", org.UserID.Int32)
		s.sendAPIErrorResponse(ctx, common.StatusOrgPermissionsError, r, w)
		return
	}

	auditEvent, err := s.BusinessDB.Impl().SoftDeleteOrganization(ctx, org, user)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete the organization", common.ErrAttr(err))
		s.sendAPIErrorResponse(ctx, common.StatusFailure, r, w)
		return
	}

	s.sendAPISuccessResponse(ctx, nil, w)

	s.BusinessDB.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourceAPI)
}
