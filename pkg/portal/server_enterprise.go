//go:build enterprise

package portal

import (
	"context"
	"fmt"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/justinas/alice"
)

func (s *Server) isEnterprise() bool {
	return true
}

func (s *Server) checkUserOrgAccess(user *dbgen.User, org *dbgen.Organization) bool {
	// NOTE: actual org ownership permissions are correctly checked in s.Org()
	return true
}

func (s *Server) checkUserOrgsLimit(ctx context.Context, user *dbgen.User, count int) bool {
	return true
}

func (s *Server) setupEnterprise(rg *RouteGenerator, privateRead, privateWrite alice.Chain) {
	arg := func(s string) string {
		return fmt.Sprintf("{%s}", s)
	}

	rg.Handle(rg.Post(common.OrgEndpoint, common.NewEndpoint), privateWrite, http.HandlerFunc(s.postNewOrg))
	rg.Handle(rg.Post(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWrite, s.Handler(s.postOrgMembers))
	rg.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint, arg(common.ParamUser)), privateWrite, http.HandlerFunc(s.deleteOrgMembers))
	rg.Handle(rg.Put(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWrite, http.HandlerFunc(s.joinOrg))
	rg.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.MembersEndpoint), privateWrite, http.HandlerFunc(s.leaveOrg))
	rg.Handle(rg.Delete(common.OrgEndpoint, arg(common.ParamOrg), common.DeleteEndpoint), privateWrite, http.HandlerFunc(s.deleteOrg))
	rg.Handle(rg.Post(common.OrgEndpoint, arg(common.ParamOrg), common.PropertyEndpoint, arg(common.ParamProperty), common.MoveEndpoint), privateWrite, http.HandlerFunc(s.moveProperty))

	rg.Handle(rg.Get(common.AuditLogsEndpoint, common.EventsEndpoint), privateRead, s.Handler(s.getAuditLogEvents))
	rg.Handle(rg.Get(common.AuditLogsEndpoint, common.ExportEndpoint), privateRead, http.HandlerFunc(s.exportAuditLogsCSV))
}
