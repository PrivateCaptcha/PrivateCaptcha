package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const (
	// Content-specific template names
	auditLogsTemplate       = "audit/audit.html"
	auditLogsEventsTemplate = "audit/events.html"
	auditLogTimeFormat      = "02 Jan 2006 15:04:05 MST"
	perPageEventLogs        = 25
)

type AuditLogsRenderContext struct {
	AuditLogs []*userAuditLog
	Count     int
	Page      int
	PerPage   int
}

type userAuditLog struct {
	UserName  string
	UserEmail string
	Action    string
	Property  string
	Resource  string
	Value     string
	TableName string
	Time      string
}

var (
	errUnexpectedAuditLogPayload = errors.New("unexpected audit log payload")
)

func (ul *userAuditLog) initFromUser(oldValue, newValue *db.AuditLogUser, planService billing.PlanService, stage string) error {
	ul.Resource = "User"

	if (oldValue != nil) && (newValue != nil) {
		if oldValue.Name != newValue.Name {
			ul.Property = "Name"
			ul.Value = newValue.Name
		} else if oldValue.Email != newValue.Email {
			ul.Property = "Email"
			ul.Value = newValue.Email
		} else if oldValue.SubscriptionID != newValue.SubscriptionID {
			ul.Property = "Subscription"
		} else if (oldValue.SubscriptionSource != newValue.SubscriptionSource) ||
			(oldValue.ExternalSubscriptionID != newValue.ExternalSubscriptionID) ||
			(oldValue.ExternalProductID != newValue.ExternalProductID) {
			ul.Property = "Subscription"

			internal := db.IsInternalSubscription(dbgen.SubscriptionSource(newValue.SubscriptionSource))
			if plan, err := planService.FindPlan(newValue.ExternalProductID, newValue.ExternalPriceID, stage, internal); err == nil {
				ul.Value = plan.Name()
			}
		}
	}

	return nil
}

func (ul *userAuditLog) initFromOrg(oldValue, newValue *db.AuditLogOrg) error {
	if (oldValue != nil) && (newValue != nil) {
		ul.Resource = "Organization"

		if oldValue.Name != newValue.Name {
			ul.Property = "Name"
			ul.Value = newValue.Name
		}
	} else {
		org := newValue
		if org == nil {
			org = oldValue
		}
		ul.Resource = fmt.Sprintf("Organization '%s'", org.Name)
	}

	return nil
}

func (ul *userAuditLog) initFromOrgUser(oldValue, newValue *db.AuditLogOrgUser) error {
	ul.Resource = "Organization"

	// for org users we don't have "classic" updates - we only create or delete
	orgUser := newValue
	if orgUser == nil {
		orgUser = oldValue
	}

	if len(orgUser.Email) > 0 {
		ul.Property = fmt.Sprintf("Member '%s'", orgUser.Email)
	} else {
		ul.Property = "Member"
	}

	ul.Resource = fmt.Sprintf("Organization '%s'", orgUser.OrgName)

	return nil
}

func (ul *userAuditLog) initFromProperty(oldValue, newValue *db.AuditLogProperty) error {
	if (oldValue != nil) && (newValue != nil) {
		ul.Resource = fmt.Sprintf("Property '%s'", oldValue.Name)
		if oldValue.Name != newValue.Name {
			ul.Property = "Name"
			ul.Value = newValue.Name
		} else if oldValue.OrgID != newValue.OrgID {
			ul.Property = "Organization"
			if len(newValue.OrgName) > 0 {
				// TODO: Actually set Org Name for Move property audit log
				ul.Value = newValue.OrgName
			}
		} else if oldValue.Level != newValue.Level {
			ul.Property = "Level"
			ul.Value = strconv.Itoa(int(newValue.Level))
		} else if oldValue.Growth != newValue.Growth {
			ul.Property = "Growth"
			ul.Value = newValue.Growth
		} else if oldValue.MaxReplayCount != newValue.MaxReplayCount {
			ul.Property = "Replay count"
			ul.Value = strconv.Itoa(int(newValue.MaxReplayCount))
		} else if oldValue.ValidityIntervalSec != newValue.ValidityIntervalSec {
			ul.Property = "Validity"
			interval := time.Duration(newValue.ValidityIntervalSec) * time.Millisecond
			ul.Value = fmt.Sprintf("%.2f hour(s)", interval.Hours())
		} else if oldValue.AllowSubdomains != newValue.AllowSubdomains {
			ul.Property = "Subdomains"
			ul.Value = strconv.FormatBool(newValue.AllowSubdomains)
		} else if oldValue.AllowLocalhost != newValue.AllowLocalhost {
			ul.Property = "Localhost"
			ul.Value = strconv.FormatBool(newValue.AllowLocalhost)
		}
	} else {
		prop := newValue
		if prop == nil {
			prop = oldValue
		}
		ul.Resource = fmt.Sprintf("Property '%s'", prop.Name)
	}

	return nil

}

func (ul *userAuditLog) initFromAPIKey(oldValue, newValue *db.AuditLogAPIKey) error {
	if (oldValue != nil) && (newValue != nil) {
		ul.Resource = fmt.Sprintf("API key %s", oldValue.Name)
		if !newValue.ExpiresAt.Time().Equal(oldValue.ExpiresAt.Time()) {
			ul.Property = "Expiration"
			ul.Value = newValue.ExpiresAt.String()
		} else if newValue.Period != oldValue.Period {
			ul.Property = "Period"
			ul.Value = strconv.Itoa(int(newValue.Period.Hours() / 24.0))
		}
	} else {
		key := newValue
		if key == nil {
			key = oldValue
		}
		ul.Resource = fmt.Sprintf("API key '%s'", key.Name)
	}

	return nil
}

func (ul *userAuditLog) initFromAccess(log *dbgen.AuditLog, payload *db.AuditLogAccess) error {
	if payload == nil {
		return errUnexpectedAuditLogPayload
	}

	ul.Property = englishCaser.String(payload.View)

	parts := strings.Split(log.EntityTable, "_")
	for i := range parts {
		parts[i] = englishCaser.String(parts[i])
	}
	resource := strings.Join(parts, " ")

	if len(payload.EntityName) > 0 {
		ul.Resource = fmt.Sprintf("'%s' (%s)", payload.EntityName, resource)
	} else {
		ul.Resource = resource
	}

	return nil
}

type mainAuditLogsRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	AuditLogsRenderContext
	From int
	To   int
	Days int
}

func (s *Server) getAuditLogs(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	renderCtx, err := s.CreateAuditLogsContext(ctx, user, 14, 0)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model:      renderCtx,
		View:       auditLogsTemplate,
		AuditEvent: newAccessAuditLogEvent(user, db.TableNameAuditLogs, int64(user.ID), "", ""),
	}, nil
}

func (s *Server) newUserAuditLog(ctx context.Context, log *dbgen.AuditLog) (*userAuditLog, error) {
	ul := &userAuditLog{
		Time:      log.CreatedAt.Time.Format(auditLogTimeFormat),
		Action:    string(log.Action),
		TableName: log.EntityTable,
	}
	var err error

	if log.Action == dbgen.AuditLogActionAccess {
		var newAccess *db.AuditLogAccess
		if _, newAccess, err = db.ParseAuditLogPayloads[db.AuditLogAccess](ctx, log); err == nil {
			err = ul.initFromAccess(log, newAccess)
		}
	} else {
		switch log.EntityTable {
		case db.TableNameUsers:
			var oldUser, newUser *db.AuditLogUser
			if oldUser, newUser, err = db.ParseAuditLogPayloads[db.AuditLogUser](ctx, log); err == nil {
				err = ul.initFromUser(oldUser, newUser, s.PlanService, s.Stage)
			}
		case db.TableNameOrgs:
			var oldOrg, newOrg *db.AuditLogOrg
			if oldOrg, newOrg, err = db.ParseAuditLogPayloads[db.AuditLogOrg](ctx, log); err == nil {
				err = ul.initFromOrg(oldOrg, newOrg)
			}
		case db.TableNameProperties:
			var oldProperty, newProperty *db.AuditLogProperty
			if oldProperty, newProperty, err = db.ParseAuditLogPayloads[db.AuditLogProperty](ctx, log); err == nil {
				err = ul.initFromProperty(oldProperty, newProperty)
			}
		case db.TableNameAPIKeys:
			var oldAPIKey, newAPIKey *db.AuditLogAPIKey
			if oldAPIKey, newAPIKey, err = db.ParseAuditLogPayloads[db.AuditLogAPIKey](ctx, log); err == nil {
				err = ul.initFromAPIKey(oldAPIKey, newAPIKey)
			}
		case db.TableNameOrgUsers:
			var oldOrgUser, newOrgUser *db.AuditLogOrgUser
			if oldOrgUser, newOrgUser, err = db.ParseAuditLogPayloads[db.AuditLogOrgUser](ctx, log); err == nil {
				err = ul.initFromOrgUser(oldOrgUser, newOrgUser)
			}
		}
	}

	if err != nil {
		return nil, err
	}

	return ul, nil
}

func (s *Server) newUserAuditLogs(ctx context.Context, user *dbgen.User, logs []*dbgen.GetUserAuditLogsRow) []*userAuditLog {
	result := make([]*userAuditLog, 0, len(logs))

	for _, log := range logs {
		if ul, err := s.newUserAuditLog(ctx, &log.AuditLog); err == nil {
			ul.UserName = user.Name
			ul.UserEmail = common.MaskEmail(user.Email, '*')
			result = append(result, ul)
		}
	}

	return result
}

func (s *Server) retrieveAuditLogs(ctx context.Context, user *dbgen.User, days int, maxLogs int) ([]*dbgen.GetUserAuditLogsRow, error) {
	slog.DebugContext(ctx, "About to retrieve audit logs", "days", days, "maxLogs", maxLogs, "userID", user.ID)
	// cache-friendly (more stable) date
	tnow := time.Now().UTC().Truncate(24 * time.Hour)
	after := tnow.AddDate(0 /*years*/, 0 /*months*/, -days)

	var allLogs []*dbgen.GetUserAuditLogsRow

	for _, cacheDays := range []int{14, 30, 90, 180, 365} {
		if cacheDays >= days {
			cachedAfter := tnow.AddDate(0 /*years*/, 0 /*months*/, -cacheDays)
			if cached, err := s.Store.Impl().GetCachedAuditLogs(ctx, user, maxLogs, after, cachedAfter); err == nil {
				slog.DebugContext(ctx, "Found cached audit logs", "cacheDays", cacheDays, "days", days, "userID", user.ID)
				allLogs = cached
				break
			}
		}
	}

	if len(allLogs) == 0 {
		var err error
		allLogs, err = s.Store.Impl().RetrieveUserAuditLogs(ctx, user, maxLogs, after)
		if err != nil {
			return nil, err
		}
	}

	return allLogs, nil
}
