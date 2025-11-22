//go:build !enterprise

package portal

import (
	"context"
	"log/slog"
	randv2 "math/rand/v2"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/justinas/alice"
)

func (s *Server) isEnterprise() bool {
	return false
}

// in not-EE environment user can only load the org they own
func (s *Server) checkUserOrgAccess(user *dbgen.User, org *dbgen.Organization) bool {
	return (user != nil) &&
		(org != nil) &&
		org.UserID.Valid &&
		(user.ID == org.UserID.Int32)
}

func (s *Server) checkUserOrgsLimit(ctx context.Context, user *dbgen.User, count int) bool {
	if count <= 1 {
		return true
	}

	if user.SubscriptionID.Valid {
		if subscription, err := s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32); err == nil {
			if plan, err := s.PlanService.FindPlan(subscription.ExternalProductID, subscription.ExternalPriceID, s.Stage,
				db.IsInternalSubscription(subscription.Source)); err == nil {
				return plan.CheckOrgsLimit(count)
			}
		}
	} else {
		slog.DebugContext(ctx, "User subscription is not valid", "userID", user.ID)
		return false
	}

	return true
}

func (s *Server) setupEnterprise(*RouteGenerator, alice.Chain, alice.Chain) {
	// BUMP
}

func auditLogsDaysFromParam(ctx context.Context, _ string) int {
	return 14
}

func maxAuditLogsForDays(days int) int {
	return 5
}

func newStubAuditLog() *userAuditLog {
	actions := []dbgen.AuditLogAction{dbgen.AuditLogActionAccess, dbgen.AuditLogActionCreate, dbgen.AuditLogActionUpdate,
		dbgen.AuditLogActionDelete, dbgen.AuditLogActionUnknown}
	tables := []string{db.TableNameProperties, db.TableNameOrgs, db.TableNameAPIKeys, db.TableNameUsers, db.TableNameOrgUsers}

	return &userAuditLog{
		UserName:  "User",
		UserEmail: "***@***.com",
		Action:    string(actions[randv2.IntN(len(actions))]),
		Property:  "",
		Resource:  "***",
		Value:     "",
		TableName: string(tables[randv2.IntN(len(tables))]),
		Time:      time.Now().Add(-time.Duration(randv2.IntN(60*24*3)) * time.Minute).Format(auditLogTimeFormat),
	}
}

func (s *Server) createOrgAuditLogsContext(ctx context.Context, org *dbgen.Organization, user *dbgen.User) (*orgAuditLogsRenderContext, *common.AuditLogEvent, error) {
	renderCtx := &orgAuditLogsRenderContext{
		AuditLogsRenderContext: AuditLogsRenderContext{},
		CurrentOrg:             orgToUserOrg(org, user.ID, s.IDHasher),
		CanView:                org.UserID.Int32 == user.ID,
	}

	const maxOrgAuditLogs = 5
	for i := 0; i < maxOrgAuditLogs; i++ {
		renderCtx.AuditLogs = append(renderCtx.AuditLogs, newStubAuditLog())
	}

	renderCtx.Count = len(renderCtx.AuditLogs)

	return renderCtx, nil, nil
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
		AuditLogsRenderContext:         AuditLogsRenderContext{},
		CanView:                        (property.CreatorID.Int32 == user.ID) || (property.OrgOwnerID.Int32 == user.ID),
	}

	renderCtx.Tab = propertyAuditLogsTabIndex

	const maxPropertyAuditLogs = 5
	for i := 0; i < maxPropertyAuditLogs; i++ {
		renderCtx.AuditLogs = append(renderCtx.AuditLogs, newStubAuditLog())
	}

	renderCtx.Count = len(renderCtx.AuditLogs)

	return renderCtx, nil, nil
}

func (s *Server) CreateAuditLogsContext(ctx context.Context, user *dbgen.User, days int, page int) (*mainAuditLogsRenderContext, error) {
	logs := make([]*userAuditLog, 0)
	const maxAuditLogs = 8
	for i := 0; i < maxAuditLogs; i++ {
		logs = append(logs, newStubAuditLog())
	}

	return &mainAuditLogsRenderContext{
		CsrfRenderContext:  s.CreateCsrfContext(user),
		AlertRenderContext: AlertRenderContext{},
		AuditLogsRenderContext: AuditLogsRenderContext{
			AuditLogs: logs,
			Count:     len(logs),
			PerPage:   perPageEventLogs,
			Page:      0,
		},
		Days: days,
		From: 1,
		To:   len(logs),
	}, nil
}
