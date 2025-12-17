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
	"github.com/justinas/alice"
)

const (
	maxAPIPostBodySize          = 128 * 1024
	maxPostPropertiesBodySize   = 1024 * 1024
	maxDeletePropertiesBodySize = 128 * 1024
)

func (s *Server) setupEnterprise(rg *common.RouteGenerator, publicChain alice.Chain, apiRateLimiter func(next http.Handler) http.Handler) {
	arg := func(s string) string {
		return fmt.Sprintf("{%s}", s)
	}

	// "portal" API
	portalAPIChain := publicChain.Append(s.Metrics.HandlerIDFunc(rg.LastPath), apiRateLimiter, monitoring.Traced, common.TimeoutHandler(5*time.Second), s.Auth.APIKey(headerAPIKey, dbgen.ApiKeyScopePortal))
	// tasks
	rg.Handle(rg.Get(common.AsyncTaskEndpoint, arg(common.ParamID)), portalAPIChain, http.HandlerFunc(s.getAsyncTask))
	// orgs
	rg.Handle(rg.Get(common.OrganizationsEndpoint), portalAPIChain, http.HandlerFunc(s.getUserOrgs))
	rg.Handle(rg.Post(common.OrgEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.postNewOrg), maxAPIPostBodySize))
	rg.Handle(rg.Put(common.OrgEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.updateOrg), maxAPIPostBodySize))
	rg.Handle(rg.Delete(common.OrgEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.deleteOrg), maxAPIPostBodySize))
	// properties
	rg.Handle(rg.Post(common.OrgEndpoint, arg(common.ParamOrg), common.PropertiesEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.postNewProperties), maxPostPropertiesBodySize))
	rg.Handle(rg.Delete(common.PropertiesEndpoint), portalAPIChain, http.MaxBytesHandler(http.HandlerFunc(s.deleteProperties), maxDeletePropertiesBodySize))
}

func (s *Server) RegisterTaskHandlers(ctx context.Context) {
	if ok := s.AsyncTasks.Register(createPropertiesHandlerID, s.handleCreateProperties); !ok {
		slog.ErrorContext(ctx, "Failed to register async task handler", "handler", createPropertiesHandlerID)
	}
	if ok := s.AsyncTasks.Register(deletePropertiesHandlerID, s.handleDeleteProperties); !ok {
		slog.ErrorContext(ctx, "Failed to register async task handler", "handler", deletePropertiesHandlerID)
	}
}

func (s *Server) requestUser(ctx context.Context) (*dbgen.User, *dbgen.APIKey, error) {
	ownerSource := &apiKeyOwnerSource{Store: s.BusinessDB, scope: dbgen.ApiKeyScopePortal}
	id, err := ownerSource.OwnerID(ctx, time.Now().UTC())
	if err != nil {
		return nil, nil, err
	}

	user, err := s.BusinessDB.Impl().RetrieveUser(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	if !user.SubscriptionID.Valid {
		return nil, nil, db.ErrNoActiveSubscription
	}

	return user, ownerSource.cachedKey, nil
}

func (s *Server) requestOrg(user *dbgen.User, r *http.Request, onlyOwner bool) (*dbgen.Organization, error) {
	ctx := r.Context()

	orgID, value, err := common.IntPathArg(r, common.ParamOrg, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse org path parameter", "value", value, common.ErrAttr(err))
		return nil, db.ErrInvalidInput
	}

	org, err := s.BusinessDB.Impl().RetrieveUserOrganization(ctx, user, orgID)
	if err != nil {
		return nil, err
	}

	if onlyOwner {
		if !org.UserID.Valid || (org.UserID.Int32 != user.ID) {
			return nil, db.ErrPermissions
		}
	}

	return org, nil
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
	case errAPIKeyScope, errInvalidAPIKey, errAPIKeyNotSet, db.ErrPermissions:
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	case db.ErrSoftDeleted:
		http.Error(w, http.StatusText(http.StatusConflict), http.StatusConflict)
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (s *Server) sendAPISuccessResponse(ctx context.Context, data interface{}, w http.ResponseWriter) {
	response := &APIResponse{
		Meta: ResponseMetadata{
			Code:        common.StatusOK,
			Description: common.StatusOK.String(),
		},
		Data: data,
	}

	if tid, ok := ctx.Value(common.TraceIDContextKey).(string); ok {
		response.Meta.RequestID = tid
	}

	common.SendJSONResponse(ctx, w, response, common.NoCacheHeaders)
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
