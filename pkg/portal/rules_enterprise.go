//go:build enterprise

package portal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/biter777/countries"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	propertyNewRuleTemplate = "property/new-rule.html"
	orgNewRuleTemplate      = "portal/org-new-rule.html"
)

type CountryOption struct {
	Code string
	Name string
}

type ruleFormData struct {
	RuleName             string
	RuleEnabled          bool
	ConditionProperty    string
	ConditionOperator    string
	ConditionNegated     bool
	ConditionValue       string
	ActionProperty       string
	ActionValue          string
	SelectedCountries    []string
}

type newRuleRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	FormData  *ruleFormData
	Countries []CountryOption
	PropertyID int32
	OrgID      int32
}

func getAllCountries() []CountryOption {
	allCountries := countries.All()
	options := make([]CountryOption, 0, len(allCountries))
	
	for _, country := range allCountries {
		if country != countries.Unknown {
			options = append(options, CountryOption{
				Code: country.Alpha2(),
				Name: country.String(),
			})
		}
	}
	
	return options
}

func (s *Server) getPropertyNewRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	property := ctx.Value(propertyContextKey).(*dbgen.Property)
	org := ctx.Value(orgContextKey).(*dbgen.Organization)

	if !s.checkUserOrgAccess(user, org) {
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	renderCtx := &newRuleRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		FormData: &ruleFormData{
			RuleEnabled:       true,
			ConditionProperty: "user_agent",
			ConditionOperator: "equals",
			ActionProperty:    "difficulty_level_percent",
		},
		Countries:  getAllCountries(),
		PropertyID: property.ID,
	}

	s.render(w, r, propertyNewRuleTemplate, renderCtx)
}

func (s *Server) getOrgNewRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org := ctx.Value(orgContextKey).(*dbgen.Organization)

	if !s.checkUserOrgAccess(user, org) {
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	renderCtx := &newRuleRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		FormData: &ruleFormData{
			RuleEnabled:       true,
			ConditionProperty: "user_agent",
			ConditionOperator: "equals",
			ActionProperty:    "difficulty_level_percent",
		},
		Countries: getAllCountries(),
		OrgID:     org.ID,
	}

	s.render(w, r, orgNewRuleTemplate, renderCtx)
}

func (s *Server) postPropertyNewRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	property := ctx.Value(propertyContextKey).(*dbgen.Property)
	org := ctx.Value(orgContextKey).(*dbgen.Organization)

	if !s.checkUserOrgAccess(user, org) {
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	rule, formData, errorMsg := s.parseRuleForm(ctx, r, property.ID, 0)
	if errorMsg != "" {
		renderCtx := &newRuleRenderContext{
			CsrfRenderContext:  s.CreateCsrfContext(user),
			AlertRenderContext: AlertRenderContext{ErrorMessage: errorMsg},
			FormData:           formData,
			Countries:          getAllCountries(),
			PropertyID:         property.ID,
		}
		s.render(w, r, propertyNewRuleTemplate, renderCtx)
		return
	}

	insertedRule, err := s.Store.Impl().InsertDifficultyRule(ctx, rule)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert difficulty rule", common.ErrAttr(err))
		renderCtx := &newRuleRenderContext{
			CsrfRenderContext:  s.CreateCsrfContext(user),
			AlertRenderContext: AlertRenderContext{ErrorMessage: "Failed to create rule. Please try again."},
			FormData:           formData,
			Countries:          getAllCountries(),
			PropertyID:         property.ID,
		}
		s.render(w, r, propertyNewRuleTemplate, renderCtx)
		return
	}

	// Record audit log
	s.RecordAuditLog(ctx, &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionCreate,
		EntityID:  int64(insertedRule.ID),
		TableName: "difficulty_rules",
		NewValue:  db.NewAuditLogDifficultyRule(insertedRule),
	}, common.AuditLogSourcePortal)

	// Redirect back to rules tab with success message
	http.Redirect(w, r, fmt.Sprintf("/org/%d/property/%d?tab=rules", org.ID, property.ID), http.StatusSeeOther)
}

func (s *Server) postOrgNewRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org := ctx.Value(orgContextKey).(*dbgen.Organization)

	if !s.checkUserOrgAccess(user, org) {
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	rule, formData, errorMsg := s.parseRuleForm(ctx, r, 0, org.ID)
	if errorMsg != "" {
		renderCtx := &newRuleRenderContext{
			CsrfRenderContext:  s.CreateCsrfContext(user),
			AlertRenderContext: AlertRenderContext{ErrorMessage: errorMsg},
			FormData:           formData,
			Countries:          getAllCountries(),
			OrgID:              org.ID,
		}
		s.render(w, r, orgNewRuleTemplate, renderCtx)
		return
	}

	insertedRule, err := s.Store.Impl().InsertDifficultyRule(ctx, rule)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert difficulty rule", common.ErrAttr(err))
		renderCtx := &newRuleRenderContext{
			CsrfRenderContext:  s.CreateCsrfContext(user),
			AlertRenderContext: AlertRenderContext{ErrorMessage: "Failed to create rule. Please try again."},
			FormData:           formData,
			Countries:          getAllCountries(),
			OrgID:              org.ID,
		}
		s.render(w, r, orgNewRuleTemplate, renderCtx)
		return
	}

	// Record audit log
	s.RecordAuditLog(ctx, &common.AuditLogEvent{
		UserID:    user.ID,
		Action:    common.AuditLogActionCreate,
		EntityID:  int64(insertedRule.ID),
		TableName: "difficulty_rules",
		NewValue:  db.NewAuditLogDifficultyRule(insertedRule),
	}, common.AuditLogSourcePortal)

	// Redirect back to rules tab with success message
	http.Redirect(w, r, fmt.Sprintf("/org/%d?tab=rules", org.ID), http.StatusSeeOther)
}

func (s *Server) parseRuleForm(ctx context.Context, r *http.Request, propertyID int32, orgID int32) (*dbgen.InsertDifficultyRuleParams, *ruleFormData, string) {
	name := strings.TrimSpace(r.FormValue(common.ParamRuleName))
	if name == "" {
		return nil, nil, "Rule name is required"
	}

	enabled := r.FormValue(common.ParamRuleEnabled) == "on"

	conditionProperty := r.FormValue(common.ParamConditionProperty)
	if conditionProperty == "" {
		return nil, nil, "Condition property is required"
	}

	conditionOperator := r.FormValue(common.ParamConditionOperator)
	conditionValue := strings.TrimSpace(r.FormValue(common.ParamConditionValue))

	// Parse operator for negation
	conditionNegated := false
	if strings.HasSuffix(conditionOperator, "_negated") {
		conditionNegated = true
		conditionOperator = strings.TrimSuffix(conditionOperator, "_negated")
	}

	// Validate based on condition property type
	var conditionValueStr pgtype.Text
	var conditionValueInt pgtype.Int4
	var conditionValueSeparator pgtype.Text

	switch conditionProperty {
	case "user_agent":
		if conditionOperator != "equals" && conditionOperator != "contains" && conditionOperator != "empty" {
			return nil, nil, "Invalid operator for user agent condition"
		}
		if conditionOperator != "empty" && conditionValue == "" {
			return nil, nil, "Condition value is required for this operator"
		}
		if conditionValue != "" {
			conditionValueStr = pgtype.Text{String: conditionValue, Valid: true}
		}

	case "ip_address":
		if conditionOperator != "matches" {
			return nil, nil, "Invalid operator for IP address condition"
		}
		if conditionValue == "" {
			return nil, nil, "IP address prefix is required"
		}
		conditionValueStr = pgtype.Text{String: conditionValue, Valid: true}

	case "country_code":
		if conditionOperator != "in" {
			return nil, nil, "Invalid operator for country code condition"
		}
		if conditionValue == "" {
			return nil, nil, "At least one country must be selected"
		}
		// Country codes are comma-separated
		conditionValueStr = pgtype.Text{String: conditionValue, Valid: true}
		conditionValueSeparator = pgtype.Text{String: ",", Valid: true}

	default:
		return nil, nil, "Invalid condition property"
	}

	actionProperty := r.FormValue(common.ParamActionProperty)
	if actionProperty == "" {
		return nil, nil, "Action property is required"
	}

	actionValueStr := r.FormValue(common.ParamActionValue)
	var actionValue int32

	switch actionProperty {
	case "difficulty_level_percent":
		if actionValueStr == "" {
			return nil, nil, "Difficulty adjustment value is required"
		}
		val, err := strconv.ParseInt(actionValueStr, 10, 32)
		if err != nil {
			return nil, nil, "Invalid difficulty adjustment value"
		}
		if val < -100 || val > 1000 {
			return nil, nil, "Difficulty adjustment must be between -100 and 1000"
		}
		actionValue = int32(val)

	case "http_request":
		// Checkbox - if checked, value is "1", otherwise empty
		if actionValueStr == "1" {
			actionValue = 1
		} else {
			actionValue = 0
		}

	case "difficulty_growth":
		if actionValueStr == "" {
			return nil, nil, "Difficulty growth value is required"
		}
		val, err := strconv.ParseInt(actionValueStr, 10, 32)
		if err != nil {
			return nil, nil, "Invalid difficulty growth value"
		}
		if val < 0 || val > 3 {
			return nil, nil, "Difficulty growth must be between 0 and 3"
		}
		actionValue = int32(val)

	default:
		return nil, nil, "Invalid action property"
	}

	// Get the current position (last position + 1)
	// For simplicity, we'll use 0 for now and let DB handle it
	position := int32(0)

	formData := &ruleFormData{
		RuleName:          name,
		RuleEnabled:       enabled,
		ConditionProperty: conditionProperty,
		ConditionOperator: conditionOperator,
		ConditionNegated:  conditionNegated,
		ConditionValue:    conditionValue,
		ActionProperty:    actionProperty,
		ActionValue:       actionValueStr,
	}

	params := &dbgen.InsertDifficultyRuleParams{
		Name:                     name,
		Enabled:                  enabled,
		ConditionProperty:        dbgen.RuleConditionProperty(conditionProperty),
		ConditionOperator:        dbgen.RuleConditionOperator(conditionOperator),
		ConditionOperatorNegated: conditionNegated,
		ConditionValueStr:        conditionValueStr,
		ConditionValueInt:        conditionValueInt,
		ConditionValueSeparator:  conditionValueSeparator,
		Position:                 position,
		ActionProperty:           dbgen.RuleActionProperty(actionProperty),
		ActionValue:              actionValue,
	}

	if propertyID > 0 {
		params.PropertyID = pgtype.Int4{Int32: propertyID, Valid: true}
		params.OrgID = pgtype.Int4{Valid: false}
	} else {
		params.OrgID = pgtype.Int4{Int32: orgID, Valid: true}
		params.PropertyID = pgtype.Int4{Valid: false}
	}

	return params, formData, ""
}
