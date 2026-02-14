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
	AuditLogs []*UserAuditLog
	Count     int
	Page      int
	PerPage   int
	SeeMore   bool
}

// UserAuditLog represents a single audit log entry for display in the UI.
// Extensions can use this type to populate custom audit log types via AuditLogParser.
type UserAuditLog struct {
	UserName  string
	UserEmail string
	Action    string
	Property  string
	Resource  string
	Value     string
	TableName string
	Time      string
	Source    string
}

var (
	errUnexpectedAuditLogPayload = errors.New("unexpected audit log payload")

	auditConst = struct {
		defaultConstants
		Days           string
		EventsEndpoint string
		ExportEndpoint string
		Page           string
	}{
		defaultConstants: defaultConst,
		Days:             common.ParamDays,
		EventsEndpoint:   common.EventsEndpoint,
		ExportEndpoint:   common.ExportEndpoint,
		Page:             common.ParamPage,
	}
)

func (ul *UserAuditLog) initFromUser(oldValue, newValue *db.AuditLogUser) error {
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
		}
	}

	return nil
}

func (ul *UserAuditLog) initFromOrg(oldValue, newValue *db.AuditLogOrg) error {
	ul.Resource = "Organization"

	if (oldValue != nil) && (newValue != nil) {
		ul.Resource = fmt.Sprintf("Organization '%s'", newValue.Name)

		if oldValue.Name != newValue.Name {
			ul.Property = "Name"
			ul.Value = newValue.Name
		} else if newValue.NewOwnerID != 0 {
			ul.Property = "Owner"
			ul.Value = common.MaskEmail(newValue.NewOwnerEmail, '*')
		}
	} else if (oldValue != nil) || (newValue != nil) {
		org := newValue
		if org == nil {
			org = oldValue
		}
		ul.Resource = fmt.Sprintf("Organization '%s'", org.Name)
	}

	return nil
}

func (ul *UserAuditLog) initFromSubscriptionPlan(sub *db.AuditLogSubscription, planService billing.PlanService, stage string) {
	ul.Property = "Product"

	internal := db.IsInternalSubscription(dbgen.SubscriptionSource(sub.Source))
	if plan, err := planService.FindPlan(sub.ExternalProductID, sub.ExternalPriceID, stage, internal); err == nil {
		priceMonthly, priceYearly := plan.PriceIDs()
		if priceMonthly == sub.ExternalPriceID {
			ul.Value = fmt.Sprintf("%s (Monthly)", plan.Name())
		} else if priceYearly == sub.ExternalPriceID {
			ul.Value = fmt.Sprintf("%s (Yearly)", plan.Name())
		} else {
			ul.Value = plan.Name()
		}
	}
}

func (ul *UserAuditLog) initFromSubscription(oldValue, newValue *db.AuditLogSubscription, planService billing.PlanService, stage string) error {
	ul.Resource = "Subscription"

	if (oldValue != nil) && (newValue != nil) {
		if oldValue.Source != newValue.Source {
			ul.Property = "Type"
			ul.Value = newValue.Source
		} else if (oldValue.ExternalProductID != newValue.ExternalProductID) ||
			(oldValue.ExternalPriceID != newValue.ExternalPriceID) {
			ul.initFromSubscriptionPlan(newValue, planService, stage)
		} else if oldValue.Status != newValue.Status {
			ul.Property = "Status"
			ul.Value = newValue.Status
		} else if !oldValue.CancelAt.Time().Equal(newValue.CancelAt.Time()) {
			ul.Property = "Cancel"
			if t := newValue.CancelAt.Time(); !t.IsZero() {
				ul.Value = t.Format("02 Jan 2006")
			}
		}
	} else if (oldValue != nil) || (newValue != nil) {
		sub := newValue
		if sub == nil {
			sub = oldValue
		}
		ul.initFromSubscriptionPlan(sub, planService, stage)
	}

	return nil
}

func (ul *UserAuditLog) initFromOrgUser(oldValue, newValue *db.AuditLogOrgUser) error {
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

func (ul *UserAuditLog) initFromProperty(oldValue, newValue *db.AuditLogProperty) error {
	ul.Resource = "Property"

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
	} else if (oldValue != nil) || (newValue != nil) {
		prop := newValue
		if prop == nil {
			prop = oldValue
		}
		ul.Resource = fmt.Sprintf("Property '%s'", prop.Name)
	}

	return nil
}

func (ul *UserAuditLog) initFromAPIKey(oldValue, newValue *db.AuditLogAPIKey) error {
	ul.Resource = "API key"

	if (oldValue != nil) && (newValue != nil) {
		ul.Resource = fmt.Sprintf("API key %s", oldValue.Name)
		if !newValue.ExpiresAt.Time().Equal(oldValue.ExpiresAt.Time()) {
			ul.Property = "Expiration"
			ul.Value = newValue.ExpiresAt.String()
		} else if newValue.Period != oldValue.Period {
			ul.Property = "Period"
			ul.Value = strconv.Itoa(int(newValue.Period.Hours() / 24.0))
		}
	} else if (oldValue != nil) || (newValue != nil) {
		key := newValue
		if key == nil {
			key = oldValue
		}
		ul.Resource = fmt.Sprintf("API key '%s'", key.Name)
	}

	return nil
}

func (ul *UserAuditLog) initFromAccess(log *dbgen.AuditLog, payload *db.AuditLogAccess) error {
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

type MainAuditLogsRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	AuditLogsRenderContext
	From int
	To   int
	Days int
}

func (c *MainAuditLogsRenderContext) Params() interface{} { return c }
func (c *MainAuditLogsRenderContext) Const() interface{}  { return auditConst }

func (s *Server) getAuditLogs(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	renderCtx, err := s.AuditLogsFunc(ctx, user, 14, 0)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model:      renderCtx,
		View:       auditLogsTemplate,
		AuditEvent: newAccessAuditLogEvent(user, db.TableNameAuditLogs, int64(user.ID), "", ""),
	}, nil
}

func (s *Server) newUserAuditLog(ctx context.Context, log *dbgen.AuditLog) (*UserAuditLog, error) {
	ul := &UserAuditLog{
		Time:      log.CreatedAt.Time.Format(auditLogTimeFormat),
		Action:    string(log.Action),
		TableName: log.EntityTable,
	}
	var err error

	if log.Source == dbgen.AuditLogSourceApi {
		ul.Source = strings.ToUpper(string(log.Source))
	} else {
		ul.Source = string(log.Source)
	}

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
				err = ul.initFromUser(oldUser, newUser)
			}
		case db.TableNameSubscriptions:
			var oldSub, newSub *db.AuditLogSubscription
			if oldSub, newSub, err = db.ParseAuditLogPayloads[db.AuditLogSubscription](ctx, log); err == nil {
				err = ul.initFromSubscription(oldSub, newSub, s.PlanService, s.Stage)
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
		default:
			// Allow extensions to handle custom audit log types
			if s.AuditLogParser != nil {
				err = s.AuditLogParser(ctx, log, ul)
			}
		}
	}

	if err != nil {
		return nil, err
	}

	return ul, nil
}

func (s *Server) newUserAuditLogs(ctx context.Context, logs []*dbgen.GetUserAuditLogsRow) []*UserAuditLog {
	result := make([]*UserAuditLog, 0, len(logs))

	for _, log := range logs {
		if ul, err := s.newUserAuditLog(ctx, &log.AuditLog); err == nil {
			if log.Name.Valid && log.Email.Valid {
				ul.UserName = log.Name.String
				ul.UserEmail = common.MaskEmail(log.Email.String, '*')
			} else {
				ul.UserName = "Unknown User"
				ul.UserEmail = "-"
			}

			result = append(result, ul)
		} else {
			slog.ErrorContext(ctx, "Failed to create audit log model", common.ErrAttr(err))
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
