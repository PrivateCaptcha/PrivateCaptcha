//go:build enterprise

package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/justinas/alice"
	easyjson "github.com/mailru/easyjson"
)

const (
	maxAPIPostBodySize          = 128 * 1024
	maxPostPropertiesBodySize   = 1024 * 1024
	maxDeletePropertiesBodySize = 128 * 1024
	maxUpdatePropertiesBodySize = 1024 * 1024
)

func (s *Server) setupEnterprise(rg *common.RouteGenerator, publicChain alice.Chain, apiRateLimiter func(next http.Handler) http.Handler) {
	arg := func(s string) string {
		return fmt.Sprintf("{%s}", s)
	}

	// "portal" API
	portalAPIChain := publicChain.Append(s.Metrics.APIHandlerIDFunc(rg.LastPath), apiRateLimiter, monitoring.Traced, common.SoftTimeoutHandler(5*time.Second), s.Auth.APIKey(headerAPIKey, dbgen.ApiKeyScopePortal))
	// tasks
	rg.Handle(rg.Get(common.AsyncTaskEndpoint, arg(common.ParamID)), portalAPIChain, http.HandlerFunc(s.getAsyncTask))
	// orgs
	rg.Handle(rg.Get(common.OrganizationsEndpoint), portalAPIChain, http.HandlerFunc(s.getUserOrgs))
	rg.Handle(rg.Post(common.OrgEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.postNewOrg), maxAPIPostBodySize))
	rg.Handle(rg.Put(common.OrgEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.updateOrg), maxAPIPostBodySize))
	rg.Handle(rg.Delete(common.OrgEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.deleteOrg), maxAPIPostBodySize))
	// properties
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertiesEndpoint), portalAPIChain, http.HandlerFunc(s.getOrgProperties))
	rg.Handle(rg.Post(common.OrgEndpoint, arg(common.ParamOrg), common.PropertiesEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.postNewProperties), maxPostPropertiesBodySize))
	rg.Handle(rg.Delete(common.PropertiesEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.deleteProperties), maxDeletePropertiesBodySize))
	rg.Handle(rg.Put(common.PropertiesEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.updateProperties), maxUpdatePropertiesBodySize))
	rg.Handle(rg.Get(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty)), portalAPIChain, http.HandlerFunc(s.getOrgProperty))
}

func (s *Server) RegisterTaskHandlers(ctx context.Context) {
	if ok := s.AsyncTasks.Register(createPropertiesHandlerID, s.handleCreateProperties); !ok {
		slog.ErrorContext(ctx, "Failed to register async task handler", "handler", createPropertiesHandlerID)
	}
	if ok := s.AsyncTasks.Register(deletePropertiesHandlerID, s.handleDeleteProperties); !ok {
		slog.ErrorContext(ctx, "Failed to register async task handler", "handler", deletePropertiesHandlerID)
	}
	if ok := s.AsyncTasks.Register(updatePropertiesHandlerID, s.handleUpdateProperties); !ok {
		slog.ErrorContext(ctx, "Failed to register async task handler", "handler", updatePropertiesHandlerID)
	}
}

func (s *Server) requestUser(ctx context.Context, readOnly bool) (*dbgen.User, *dbgen.APIKey, error) {
	return s.requestUserEx(ctx, readOnly, true /*requiresSubscription*/)
}

func (s *Server) requestUserEx(ctx context.Context, readOnly bool, requiresSubscription bool) (*dbgen.User, *dbgen.APIKey, error) {
	portalOwnerSource := &apiKeyOwnerSource{Store: s.BusinessDB, scope: dbgen.ApiKeyScopePortal}
	id, _, err := portalOwnerSource.OwnerID(ctx, time.Now().UTC())
	if err != nil {
		return nil, nil, err
	}

	if portalOwnerSource.cachedKey != nil && (portalOwnerSource.cachedKey.Readonly && !readOnly) {
		slog.WarnContext(ctx, "API key read-write attribute does not match", "expected", readOnly,
			"actual", portalOwnerSource.cachedKey.Readonly)
		return nil, nil, errAPIKeyReadOnly
	}

	user, err := s.BusinessDB.Impl().RetrieveUser(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	if requiresSubscription && !user.SubscriptionID.Valid {
		return nil, nil, db.ErrNoActiveSubscription
	}

	return user, portalOwnerSource.cachedKey, nil
}

func (s *Server) requestOrg(user *dbgen.User, r *http.Request, onlyOwner bool, allowedOrgID *pgtype.Int4) (*dbgen.Organization, dbgen.NullAccessLevel, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return nil, dbgen.NullAccessLevel{}, db.ErrInvalidInput
	}

	if (allowedOrgID != nil) && allowedOrgID.Valid && (allowedOrgID.Int32 != orgID) {
		slog.WarnContext(ctx, "Requested organization is not allowed for this requester", "allowedOrgID", allowedOrgID.Int32, "requestedOrgID", orgID)
		return nil, dbgen.NullAccessLevel{}, db.ErrPermissions
	}

	org, level, err := s.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, orgID)
	if err != nil {
		return nil, dbgen.NullAccessLevel{}, err
	}

	if onlyOwner {
		if !org.UserID.Valid || (org.UserID.Int32 != user.ID) {
			return nil, dbgen.NullAccessLevel{}, db.ErrPermissions
		}
	}

	return org, level, nil
}

func (s *Server) requestProperty(org *dbgen.Organization, r *http.Request) (*dbgen.Property, error) {
	ctx := r.Context()

	propertyID, value, err := common.IntPathArg(r, common.ParamProperty, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse property path parameter", "value", value, common.ErrAttr(err))
		return nil, db.ErrInvalidInput
	}

	property, err := s.BusinessDB.Impl().RetrieveOrgProperty(ctx, org, propertyID)
	if err != nil {
		return nil, err
	}

	if !property.Enabled {
		slog.WarnContext(ctx, "Property is disabled", "propID", property.ID, "domain", property.Domain)
		return nil, db.ErrDisabled
	}

	return property, nil
}

func (s *Server) sendHTTPErrorResponse(err error, w http.ResponseWriter) {
	switch err {
	case db.ErrRecordNotFound:
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	case db.ErrInvalidInput:
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	case db.ErrNoActiveSubscription:
		http.Error(w, http.StatusText(http.StatusPaymentRequired), http.StatusPaymentRequired)
	case db.ErrMaintenance:
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
	case errAPIKeyScope, errInvalidAPIKey, errAPIKeyNotSet, errAPIKeyReadOnly, db.ErrPermissions:
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	case db.ErrSoftDeleted:
		http.Error(w, http.StatusText(http.StatusConflict), http.StatusConflict)
	case context.DeadlineExceeded:
		http.Error(w, http.StatusText(http.StatusGatewayTimeout), http.StatusGatewayTimeout)
	case context.Canceled:
		// Client disconnected, no need to send a response
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (s *Server) sendAPISuccessResponse(ctx context.Context, data easyjson.Marshaler, w http.ResponseWriter) {
	s.sendAPISuccessResponseEx(ctx, &APIResponse{Data: data}, w, common.NoCacheHeaders)
}

func (s *Server) sendAPISuccessResponseEx(ctx context.Context, response *APIResponse, w http.ResponseWriter, headers ...map[string][]string) {
	response.Meta = ResponseMetadata{
		Code:        common.StatusOK,
		Description: common.StatusOK.String(),
	}

	if tid, ok := ctx.Value(common.TraceIDContextKey).(string); ok {
		response.Meta.RequestID = tid
	}

	common.SendJSONResponse(ctx, w, response, headers...)
}

func (s *Server) sendAPIErrorResponse(ctx context.Context, code common.StatusCode, r *http.Request, w http.ResponseWriter) {
	response := &APIResponse{
		Meta: ResponseMetadata{
			Code:        code,
			Description: code.String(),
		},
	}

	if tid, ok := ctx.Value(common.TraceIDContextKey).(string); ok {
		response.Meta.RequestID = tid
	}

	common.SendJSONResponse(ctx, w, response, common.NoCacheHeaders)

	slog.WarnContext(ctx, "Returned API error response", "code", int(code))

	s.Metrics.ObserveApiError(r.URL.Path, r.Method, int(code))
}

func (s *Server) retrievePropertyRules(ctx context.Context, property *dbgen.Property) *rules.RulesPair {
	impl := s.BusinessDB.Impl()
	needsBackfill := false

	var propertyRules *rules.CompiledRules
	if cached, needsRefresh, err := impl.GetCachedCompiledPropertyRules(ctx, property.ID); err == nil {
		propertyRules = cached
		if needsRefresh {
			needsBackfill = true
		}
	} else if err == db.ErrCacheMiss {
		needsBackfill = true
	} else if err != db.ErrNegativeCacheHit {
		slog.ErrorContext(ctx, "Failed to load cached compiled property rules", "propertyID", property.ID, common.ErrAttr(err))
	}

	var orgRules *rules.CompiledRules
	if property.OrgID.Valid {
		if cached, needsRefresh, err := impl.GetCachedCompiledOrgRules(ctx, property.OrgID.Int32); err == nil {
			orgRules = cached
			if needsRefresh {
				needsBackfill = true
			}
		} else if err == db.ErrCacheMiss {
			needsBackfill = true
		} else if err != db.ErrNegativeCacheHit {
			slog.ErrorContext(ctx, "Failed to load cached compiled org rules", "orgID", property.OrgID.Int32, common.ErrAttr(err))
		}
	}

	if needsBackfill {
		s.Auth.RefreshPropertyRules(ctx, property.ID)
	}

	return &rules.RulesPair{
		PropertyRules: propertyRules,
		OrgRules:      orgRules,
	}
}

func (am *AuthMiddleware) RefreshPropertyRules(ctx context.Context, propertyID int32) {
	timer := time.NewTimer(am.backpressureTimeout)
	defer timer.Stop()

	select {
	case am.RulesChan <- propertyID:
	case <-ctx.Done():
		slog.WarnContext(ctx, "Context cancelled for property rules refresh", "propertyID", propertyID)
	case <-timer.C:
		am.Metrics.ObserveEventDropped(common.PropertyRulesEventType)
	}
}
