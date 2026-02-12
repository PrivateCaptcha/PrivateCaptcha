//go:build !enterprise

package portal

import (
	"context"
	randv2 "math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/justinas/alice"
)

func stubDifficultyRules() []*DifficultyRuleModel {
	return []*DifficultyRuleModel{
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:                    db.Text("Block suspicious countries"),
			Enabled:                 true,
			ConditionProperty:       dbgen.RuleConditionPropertyCountryCode,
			ConditionOperator:       dbgen.RuleConditionOperatorIn,
			ConditionValueStr:       db.Text("CN, RU, KP"),
			ConditionValueSeparator: db.Text(","),
			Position:                0,
			ActionProperty:          dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:             0,
			CreatedAt:               db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:               db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}),
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:              db.Text("Block empty User-Agents"),
			Enabled:           false,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorEmpty,
			Position:          1,
			ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:       0,
			CreatedAt:         db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:         db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}),
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:              db.Text("Lower difficulty for mobile"),
			Enabled:           true,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: db.Text("Mobile"),
			Position:          2,
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       120,
			CreatedAt:         db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:         db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}),
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:              db.Text("Raise difficulty for crawlers"),
			Enabled:           true,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorContains,
			ConditionValueStr: db.Text("curl"),
			Position:          3,
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       180,
			CreatedAt:         db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:         db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}),
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:              db.Text("Lower difficulty for trusted IPs"),
			Enabled:           true,
			ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator: dbgen.RuleConditionOperatorMatches,
			ConditionValueStr: db.Text("192.168.0.0/16"),
			Position:          4,
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevel,
			ActionValue:       120,
			CreatedAt:         db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:         db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}),
	}
}

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
		if subscr, err := s.Store.Impl().RetrieveSubscription(ctx, user.SubscriptionID.Int32); err == nil {
			if ok, _, err := s.SubscriptionLimits.CheckOrgsLimit(ctx, user.ID, subscr); err == nil {
				return ok
			}
		}
	}

	return true
}

func (s *Server) setupEnterprise(*common.RouteGenerator, alice.Chain, alice.Chain, alice.Chain) {
	// BUMP
}

func auditLogsDaysFromParam(ctx context.Context, _ string) int {
	return 14
}

func maxAuditLogsForDays(days int) int {
	return 5
}

func MaxAuditLogsRetention(cfg common.ConfigStore) time.Duration {
	return 14 * 24 * time.Hour
}

func newStubAuditLog() *UserAuditLog {
	actions := []dbgen.AuditLogAction{dbgen.AuditLogActionAccess, dbgen.AuditLogActionCreate, dbgen.AuditLogActionUpdate,
		dbgen.AuditLogActionDelete, dbgen.AuditLogActionUnknown}
	tables := []string{db.TableNameProperties, db.TableNameOrgs, db.TableNameAPIKeys, db.TableNameUsers, db.TableNameOrgUsers}
	sources := []string{string(dbgen.AuditLogSourcePortal), strings.ToUpper(string(dbgen.AuditLogSourceApi))}

	return &UserAuditLog{
		UserName:  "User",
		UserEmail: "***@***.com",
		Action:    string(actions[randv2.IntN(len(actions))]),
		Source:    sources[randv2.IntN(len(sources))],
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

func (s *Server) CreateAuditLogsContext(ctx context.Context, user *dbgen.User, days int, page int) (*MainAuditLogsRenderContext, error) {
	logs := make([]*UserAuditLog, 0)
	const maxAuditLogs = 8
	for i := 0; i < maxAuditLogs; i++ {
		logs = append(logs, newStubAuditLog())
	}

	return &MainAuditLogsRenderContext{
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

func (s *Server) getPropertyRules(w http.ResponseWriter, r *http.Request) (*propertyRulesRenderContext, *common.AuditLogEvent, error) {
	dashboardCtx, _, err := s.getOrgProperty(w, r)
	if err != nil {
		return nil, nil, err
	}

	renderCtx := &propertyRulesRenderContext{
		propertyDashboardRenderContext: *dashboardCtx,
		Rules:                          stubDifficultyRules(),
	}

	renderCtx.Tab = propertyRulesTabIndex

	return renderCtx, nil, nil
}

func (s *Server) createOrgRulesContext(ctx context.Context, org *dbgen.Organization, user *dbgen.User) (*orgRulesRenderContext, *common.AuditLogEvent, error) {
	renderCtx := &orgRulesRenderContext{
		CurrentOrg: orgToUserOrg(org, user.ID, s.IDHasher),
		Rules:      stubDifficultyRules(),
	}

	return renderCtx, nil, nil
}
