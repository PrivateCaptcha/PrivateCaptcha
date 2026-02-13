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
	FormData             *ruleFormData
	Countries            []CountryOption
	PropertyID           string
	OrgID                string
	ConditionProperties  []string
	StringOperators      []string
	IPOperators          []string
	ActionProperties     []string
	GrowthTypes          []dbgen.DifficultyGrowth
	ConditionUserAgent   string
	ConditionIPAddress   string
	ConditionCountryCode string
	OperatorEquals       string
	OperatorContains     string
	OperatorEmpty        string
	OperatorMatches      string
	OperatorIn           string
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

func populateRuleConstants(renderCtx *newRuleRenderContext) {
	renderCtx.ConditionProperties = []string{
		string(dbgen.RuleConditionPropertyUserAgent),
		string(dbgen.RuleConditionPropertyIPAddress),
		string(dbgen.RuleConditionPropertyCountryCode),
	}

	renderCtx.StringOperators = []string{
		string(dbgen.RuleConditionOperatorEquals),
		string(dbgen.RuleConditionOperatorContains),
		string(dbgen.RuleConditionOperatorEmpty),
	}

	renderCtx.IPOperators = []string{
		string(dbgen.RuleConditionOperatorMatches),
		string(dbgen.RuleConditionOperatorEmpty),
	}

	renderCtx.ActionProperties = []string{
		string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		string(dbgen.RuleActionPropertyHTTPRequest),
		string(dbgen.RuleActionPropertyDifficultyGrowth),
	}

	renderCtx.GrowthTypes = []dbgen.DifficultyGrowth{
		dbgen.DifficultyGrowthConstant,
		dbgen.DifficultyGrowthSlow,
		dbgen.DifficultyGrowthMedium,
		dbgen.DifficultyGrowthFast,
	}

	renderCtx.ConditionUserAgent = string(dbgen.RuleConditionPropertyUserAgent)
	renderCtx.ConditionIPAddress = string(dbgen.RuleConditionPropertyIPAddress)
	renderCtx.ConditionCountryCode = string(dbgen.RuleConditionPropertyCountryCode)
	renderCtx.OperatorEquals = string(dbgen.RuleConditionOperatorEquals)
	renderCtx.OperatorContains = string(dbgen.RuleConditionOperatorContains)
	renderCtx.OperatorEmpty = string(dbgen.RuleConditionOperatorEmpty)
	renderCtx.OperatorMatches = string(dbgen.RuleConditionOperatorMatches)
	renderCtx.OperatorIn = string(dbgen.RuleConditionOperatorIn)
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
	populateRuleConstants(renderCtx)

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
	populateRuleConstants(renderCtx)

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
	populateRuleConstants(renderCtx)

	rule, formData, statusCode := s.parseRuleForm(ctx, r, property.ID, 0)
	renderCtx.FormData = formData

	if !statusCode.Success() {
		renderCtx.ErrorMessage = statusCode.String()
		s.render(w, r, propertyNewRuleTemplate, renderCtx)
		return
	}

	_, auditEvent, err := s.Store.Impl().CreateDifficultyRule(ctx, user, rule)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert difficulty rule", common.ErrAttr(err))
		renderCtx.ErrorMessage = common.StatusFailure.String()
		s.render(w, r, propertyNewRuleTemplate, renderCtx)
		return
	}

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	// Redirect back to rules tab with success message
	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertyEndpoint, s.IDHasher.Encrypt(int(property.ID)))+"?"+common.ParamTab+"="+common.RulesEndpoint, http.StatusOK, w, r)
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
	populateRuleConstants(renderCtx)

	rule, formData, statusCode := s.parseRuleForm(ctx, r, 0, org.ID)
	renderCtx.FormData = formData

	if !statusCode.Success() {
		renderCtx.ErrorMessage = statusCode.String()
		s.render(w, r, orgNewRuleTemplate, renderCtx)
		return
	}

	_, auditEvent, err := s.Store.Impl().CreateDifficultyRule(ctx, user, rule)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert difficulty rule", common.ErrAttr(err))
		renderCtx.ErrorMessage = common.StatusFailure.String()
		s.render(w, r, orgNewRuleTemplate, renderCtx)
		return
	}

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	// Redirect back to rules tab with success message
	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)))+"?"+common.ParamTab+"="+common.RulesEndpoint, http.StatusOK, w, r)
}

func (s *Server) parseRuleForm(ctx context.Context, r *http.Request, propertyID int32, orgID int32) (*dbgen.CreateDifficultyRuleParams, *ruleFormData, common.StatusCode) {
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
	var conditionValueStr pgtype.Text
	var conditionValueInt pgtype.Int4
	var conditionValueSeparator pgtype.Text
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

	params := &dbgen.CreateDifficultyRuleParams{
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
		params.PropertyID = db.Int(propertyID)
		params.OrgID = db.NullInt()
	} else {
		params.OrgID = db.Int(orgID)
		params.PropertyID = db.NullInt()
	}

	return params, formData, common.StatusOK
}

func (s *Server) parseUserAgentCondition(operator string, value string) (pgtype.Text, common.StatusCode) {
	// Validate operator
	switch operator {
	case string(dbgen.RuleConditionOperatorEquals),
		string(dbgen.RuleConditionOperatorContains),
		string(dbgen.RuleConditionOperatorEmpty):
	// Valid operators
	default:
		return pgtype.Text{Valid: false}, common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if operator != string(dbgen.RuleConditionOperatorEmpty) && value == "" {
		return pgtype.Text{Valid: false}, common.StatusRuleConditionValueRequired
	}

	if value != "" {
		return pgtype.Text{String: value, Valid: true}, common.StatusOK
	}

	return pgtype.Text{Valid: false}, common.StatusOK
}

func (s *Server) parseIPAddressCondition(operator string, value string) (pgtype.Text, common.StatusCode) {
	// Validate operator - IP address can use matches or empty
	switch operator {
	case string(dbgen.RuleConditionOperatorMatches),
		string(dbgen.RuleConditionOperatorEmpty):
	// Valid operators
	default:
		return pgtype.Text{Valid: false}, common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if operator != string(dbgen.RuleConditionOperatorEmpty) && value == "" {
		return pgtype.Text{Valid: false}, common.StatusRuleIPAddressRequired
	}

	if value != "" {
		return pgtype.Text{String: value, Valid: true}, common.StatusOK
	}

	return pgtype.Text{Valid: false}, common.StatusOK
}

func (s *Server) parseCountryCodeCondition(operator string, value string) (pgtype.Text, pgtype.Text, common.StatusCode) {
	// Validate operator
	if operator != string(dbgen.RuleConditionOperatorIn) {
		return pgtype.Text{Valid: false}, pgtype.Text{}, common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if value == "" {
		return pgtype.Text{Valid: false}, pgtype.Text{}, common.StatusRuleCountryRequired
	}

	// Country codes are comma-separated
	return pgtype.Text{String: value, Valid: true}, pgtype.Text{String: ",", Valid: true}, common.StatusOK
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
