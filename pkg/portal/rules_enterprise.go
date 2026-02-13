//go:build enterprise

package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/biter777/countries"
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
	RuleName          string
	RuleEnabled       bool
	ConditionProperty string
	ConditionOperator string
	ConditionNegated  bool
	ConditionValue    string
	ActionProperty    string
	ActionValue       string
}

type newRuleRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	FormData   *ruleFormData
	Countries  []CountryOption
	PropertyID string
	OrgID      string
}

var (
	cachedCountries []CountryOption
	countriesOnce   sync.Once
)

func getAllCountries() []CountryOption {
	countriesOnce.Do(func() {
		allCountries := countries.All()
		cachedCountries = make([]CountryOption, 0, len(allCountries))

		for _, country := range allCountries {
			if country != countries.Unknown {
				cachedCountries = append(cachedCountries, CountryOption{
					Code: country.Alpha2(),
					Name: country.String(),
				})
			}
		}
	})

	return cachedCountries
}

func (s *Server) getPropertyNewRule(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	property, err := s.Property(org, r)
	if err != nil {
		return nil, err
	}

	if !s.checkUserOrgAccess(user, org) {
		return nil, db.ErrPermissions
	}

	renderCtx := &newRuleRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		FormData: &ruleFormData{
			RuleEnabled:       true,
			ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
			ConditionOperator: string(dbgen.RuleConditionOperatorEquals),
			ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		},
		Countries:  getAllCountries(),
		PropertyID: s.IDHasher.Encrypt(int(property.ID)),
	}

	return &ViewModel{Model: renderCtx, View: propertyNewRuleTemplate}, nil
}

func (s *Server) getOrgNewRule(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	if !s.checkUserOrgAccess(user, org) {
		return nil, db.ErrPermissions
	}

	renderCtx := &newRuleRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		FormData: &ruleFormData{
			RuleEnabled:       true,
			ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
			ConditionOperator: string(dbgen.RuleConditionOperatorEquals),
			ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		},
		Countries: getAllCountries(),
		OrgID:     s.IDHasher.Encrypt(int(org.ID)),
	}

	return &ViewModel{Model: renderCtx, View: orgNewRuleTemplate}, nil
}

func (s *Server) postPropertyNewRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	property, err := s.Property(org, r)
	if err != nil {
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	renderCtx := &newRuleRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		Countries:         getAllCountries(),
		PropertyID:        s.IDHasher.Encrypt(int(property.ID)),
	}

	rule, formData, statusCode := s.parseRuleForm(ctx, r, property.ID, 0)
	renderCtx.FormData = formData

	if !statusCode.Success() {
		renderCtx.ErrorMessage = statusCode.String()
		s.render(w, r, propertyNewRuleTemplate, renderCtx)
		return
	}

	_, auditEvent, err := s.Store.Impl().InsertDifficultyRule(ctx, rule)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert difficulty rule", common.ErrAttr(err))
		renderCtx.ErrorMessage = common.StatusFailure.String()
		s.render(w, r, propertyNewRuleTemplate, renderCtx)
		return
	}

	auditEvent.UserID = user.ID
	auditEvent.Action = common.AuditLogActionCreate
	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	// Redirect back to rules tab with success message
	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertyEndpoint, s.IDHasher.Encrypt(int(property.ID)))+"?tab=rules", http.StatusOK, w, r)
}

func (s *Server) postOrgNewRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, level, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	// Only org owner can create org-level rules
	if !level.Valid || level.AccessLevel != dbgen.AccessLevelOwner {
		slog.WarnContext(ctx, "User is not org owner, cannot create org rules", "orgID", org.ID, "userID", user.ID)
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	renderCtx := &newRuleRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		Countries:         getAllCountries(),
		OrgID:             s.IDHasher.Encrypt(int(org.ID)),
	}

	rule, formData, statusCode := s.parseRuleForm(ctx, r, 0, org.ID)
	renderCtx.FormData = formData

	if !statusCode.Success() {
		renderCtx.ErrorMessage = statusCode.String()
		s.render(w, r, orgNewRuleTemplate, renderCtx)
		return
	}

	_, auditEvent, err := s.Store.Impl().InsertDifficultyRule(ctx, rule)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert difficulty rule", common.ErrAttr(err))
		renderCtx.ErrorMessage = common.StatusFailure.String()
		s.render(w, r, orgNewRuleTemplate, renderCtx)
		return
	}

	auditEvent.UserID = user.ID
	auditEvent.Action = common.AuditLogActionCreate
	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	// Redirect back to rules tab with success message
	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)))+"?tab=rules", http.StatusOK, w, r)
}

func (s *Server) parseRuleForm(ctx context.Context, r *http.Request, propertyID int32, orgID int32) (*dbgen.InsertDifficultyRuleParams, *ruleFormData, common.StatusCode) {
	name := strings.TrimSpace(r.FormValue(common.ParamRuleName))
	if name == "" {
		return nil, nil, common.StatusRuleNameEmptyError
	}

	enabled := r.FormValue(common.ParamRuleEnabled) == "on"

	conditionProperty := r.FormValue(common.ParamConditionProperty)
	if conditionProperty == "" {
		return nil, nil, common.StatusRuleConditionPropertyRequired
	}

	conditionOperator := r.FormValue(common.ParamConditionOperator)
	conditionValue := strings.TrimSpace(r.FormValue(common.ParamConditionValue))

	// Parse operator for negation
	conditionNegated := false
	if strings.HasSuffix(conditionOperator, "_negated") {
		conditionNegated = true
		conditionOperator = strings.TrimSuffix(conditionOperator, "_negated")
	}

	formData := &ruleFormData{
		RuleName:          name,
		RuleEnabled:       enabled,
		ConditionProperty: conditionProperty,
		ConditionOperator: conditionOperator,
		ConditionNegated:  conditionNegated,
		ConditionValue:    conditionValue,
	}

	// Validate and parse based on condition property type
	var conditionValueStr db.NullText
	var conditionValueInt db.NullInt4
	var conditionValueSeparator db.NullText
	var parseStatus common.StatusCode

	switch conditionProperty {
	case string(dbgen.RuleConditionPropertyUserAgent):
		conditionValueStr, parseStatus = s.parseUserAgentCondition(conditionOperator, conditionValue)
	case string(dbgen.RuleConditionPropertyIPAddress):
		conditionValueStr, parseStatus = s.parseIPAddressCondition(conditionOperator, conditionValue)
	case string(dbgen.RuleConditionPropertyCountryCode):
		conditionValueStr, conditionValueSeparator, parseStatus = s.parseCountryCodeCondition(conditionOperator, conditionValue)
	default:
		return nil, formData, common.StatusRuleConditionPropertyInvalid
	}

	if !parseStatus.Success() {
		return nil, formData, parseStatus
	}

	actionProperty := r.FormValue(common.ParamActionProperty)
	if actionProperty == "" {
		return nil, formData, common.StatusRuleActionPropertyRequired
	}

	actionValueStr := r.FormValue(common.ParamActionValue)
	formData.ActionProperty = actionProperty
	formData.ActionValue = actionValueStr

	var actionValue int32
	var actionStatus common.StatusCode

	switch actionProperty {
	case string(dbgen.RuleActionPropertyDifficultyLevelPercent):
		actionValue, actionStatus = s.parseDifficultyAction(actionValueStr)
	case string(dbgen.RuleActionPropertyHTTPRequest):
		actionValue, actionStatus = s.parseHTTPRequestAction(actionValueStr)
	case string(dbgen.RuleActionPropertyDifficultyGrowth):
		actionValue, actionStatus = s.parseDifficultyGrowthAction(actionValueStr)
	default:
		return nil, formData, common.StatusRuleActionPropertyInvalid
	}

	if !actionStatus.Success() {
		return nil, formData, actionStatus
	}

	// Get the current position (last position + 1)
	// For simplicity, we'll use 0 for now and let DB handle it
	position := int32(0)

	params := &dbgen.InsertDifficultyRuleParams{
		Name:                     name,
		Enabled:                  enabled,
		ConditionProperty:        dbgen.RuleConditionProperty(conditionProperty),
		ConditionOperator:        dbgen.RuleConditionOperator(conditionOperator),
		ConditionOperatorNegated: conditionNegated,
		ConditionValueStr:        conditionValueStr.ToDBType(),
		ConditionValueInt:        conditionValueInt.ToDBType(),
		ConditionValueSeparator:  conditionValueSeparator.ToDBType(),
		Position:                 position,
		ActionProperty:           dbgen.RuleActionProperty(actionProperty),
		ActionValue:              actionValue,
	}

	if propertyID > 0 {
		params.PropertyID = db.Int(propertyID)
		params.OrgID = db.InvalidInt
	} else {
		params.OrgID = db.Int(orgID)
		params.PropertyID = db.InvalidInt
	}

	return params, formData, common.StatusOK
}

func (s *Server) parseUserAgentCondition(operator string, value string) (db.NullText, common.StatusCode) {
	// Validate operator
	switch operator {
	case string(dbgen.RuleConditionOperatorEquals),
		string(dbgen.RuleConditionOperatorContains),
		string(dbgen.RuleConditionOperatorEmpty):
	// Valid operators
	default:
		return db.NullText{}, common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if operator != string(dbgen.RuleConditionOperatorEmpty) && value == "" {
		return db.NullText{}, common.StatusRuleConditionValueRequired
	}

	if value != "" {
		return db.NullText{String: value, Valid: true}, common.StatusOK
	}

	return db.NullText{}, common.StatusOK
}

func (s *Server) parseIPAddressCondition(operator string, value string) (db.NullText, common.StatusCode) {
	// Validate operator - IP address can use matches or empty
	switch operator {
	case string(dbgen.RuleConditionOperatorMatches),
		string(dbgen.RuleConditionOperatorEmpty):
	// Valid operators
	default:
		return db.NullText{}, common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if operator != string(dbgen.RuleConditionOperatorEmpty) && value == "" {
		return db.NullText{}, common.StatusRuleIPAddressRequired
	}

	if value != "" {
		return db.NullText{String: value, Valid: true}, common.StatusOK
	}

	return db.NullText{}, common.StatusOK
}

func (s *Server) parseCountryCodeCondition(operator string, value string) (db.NullText, db.NullText, common.StatusCode) {
	// Validate operator
	if operator != string(dbgen.RuleConditionOperatorIn) {
		return db.NullText{}, db.NullText{}, common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if value == "" {
		return db.NullText{}, db.NullText{}, common.StatusRuleCountryRequired
	}

	// Country codes are comma-separated
	return db.NullText{String: value, Valid: true}, db.NullText{String: ",", Valid: true}, common.StatusOK
}

func (s *Server) parseDifficultyAction(valueStr string) (int32, common.StatusCode) {
	if valueStr == "" {
		return 0, common.StatusRuleActionValueRequired
	}

	val, err := strconv.ParseInt(valueStr, 10, 32)
	if err != nil {
		return 0, common.StatusRuleActionValueInvalid
	}

	if val < -100 || val > 1000 {
		return 0, common.StatusRuleDifficultyValueInvalid
	}

	return int32(val), common.StatusOK
}

func (s *Server) parseHTTPRequestAction(valueStr string) (int32, common.StatusCode) {
	// Checkbox - if checked, value is "1", otherwise empty
	if valueStr == "1" {
		return 1, common.StatusOK
	}
	return 0, common.StatusOK
}

func (s *Server) parseDifficultyGrowthAction(valueStr string) (int32, common.StatusCode) {
	if valueStr == "" {
		return 0, common.StatusRuleActionValueRequired
	}

	val, err := strconv.ParseInt(valueStr, 10, 32)
	if err != nil {
		return 0, common.StatusRuleActionValueInvalid
	}

	if val < 0 || val > 3 {
		return 0, common.StatusRuleDifficultyGrowthInvalid
	}

	return int32(val), common.StatusOK
}
