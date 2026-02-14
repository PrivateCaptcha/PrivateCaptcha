//go:build enterprise

package portal

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
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
	ruleTemplate     = "rules/rule.html"
	ruleFormTemplate = "rules/form.html"
)

var (
	ruleConstants = RuleConstants{
		BaseRenderConstants:            baseConst,
		DashboardEndpoint:              common.DashboardEndpoint,
		OrgEndpoint:                    common.OrgEndpoint,
		PropertyEndpoint:               common.PropertyEndpoint,
		RulesEndpoint:                  common.RulesEndpoint,
		NewEndpoint:                    common.NewEndpoint,
		ConditionUserAgent:             string(dbgen.RuleConditionPropertyUserAgent),
		ConditionIPAddress:             string(dbgen.RuleConditionPropertyIPAddress),
		ConditionCountryCode:           string(dbgen.RuleConditionPropertyCountryCode),
		OperatorEquals:                 string(dbgen.RuleConditionOperatorEquals),
		OperatorContains:               string(dbgen.RuleConditionOperatorContains),
		OperatorEmpty:                  string(dbgen.RuleConditionOperatorEmpty),
		OperatorMatches:                string(dbgen.RuleConditionOperatorMatches),
		OperatorIn:                     string(dbgen.RuleConditionOperatorIn),
		StringOperators:                []string{string(dbgen.RuleConditionOperatorEquals), string(dbgen.RuleConditionOperatorContains), string(dbgen.RuleConditionOperatorEmpty)},
		IPOperators:                    []string{string(dbgen.RuleConditionOperatorMatches), string(dbgen.RuleConditionOperatorEmpty)},
		ConditionProperty:              common.ParamConditionProperty,
		ConditionPropertyUserAgent:     string(dbgen.RuleConditionPropertyUserAgent),
		ConditionPropertyIPAddress:     string(dbgen.RuleConditionPropertyIPAddress),
		ConditionPropertyCountryCode:   string(dbgen.RuleConditionPropertyCountryCode),
		GrowthTypeConstant:             string(dbgen.DifficultyGrowthConstant),
		GrowthTypeSlow:                 string(dbgen.DifficultyGrowthSlow),
		GrowthTypeMedium:               string(dbgen.DifficultyGrowthMedium),
		GrowthTypeFast:                 string(dbgen.DifficultyGrowthFast),
		ActionPropertyDifficultyLevel:  string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		ActionPropertyDifficultyGrowth: string(dbgen.RuleActionPropertyDifficultyGrowth),
		ActionPropertyHTTPRequest:      string(dbgen.RuleActionPropertyHTTPRequest),
		ActionProperty:                 common.ParamActionProperty,
		ConditionOperator:              common.ParamConditionOperator,
		ConditionValue:                 common.ParamConditionValue,
		ActionValue:                    common.ParamActionValue,
		Name:                           common.ParamName,
		Enabled:                        common.ParamEnabled,
		OrgLevelOwner:                  string(dbgen.AccessLevelOwner),
		OrgLevelInvited:                string(dbgen.AccessLevelInvited),
		OrgLevelMember:                 string(dbgen.AccessLevelMember),
		MembersEndpoint:                common.MembersEndpoint,
		ReportsEndpoint:                common.ReportsEndpoint,
		IntegrationsEndpoint:           common.IntegrationsEndpoint,
		EventsEndpoint:                 common.EventsEndpoint,
		Tab:                            common.ParamTab,
		EditEndpoint:                   common.EditEndpoint,
		ConditionNegated:               "condition_negated",
	}
)

type CountryOption struct {
	Code string
	Name string
}

type RuleFormData struct {
	Name              string
	NameError         string
	ConditionProperty string
	ConditionOperator string
	ConditionValue    string
	ActionProperty    string
	ActionValue       string
	Enabled           bool
	ConditionNegated  bool
}

type RuleConstants struct {
	BaseRenderConstants

	DashboardEndpoint              string
	OrgEndpoint                    string
	PropertyEndpoint               string
	RulesEndpoint                  string
	NewEndpoint                    string
	ConditionUserAgent             string
	ConditionIPAddress             string
	ConditionCountryCode           string
	ConditionProperty              string
	ConditionOperator              string
	ConditionValue                 string
	ActionProperty                 string
	ActionValue                    string
	ConditionPropertyUserAgent     string
	ConditionPropertyIPAddress     string
	ConditionPropertyCountryCode   string
	OperatorEquals                 string
	OperatorContains               string
	OperatorEmpty                  string
	OperatorMatches                string
	OperatorIn                     string
	StringOperators                []string
	IPOperators                    []string
	GrowthTypeConstant             string
	GrowthTypeSlow                 string
	GrowthTypeMedium               string
	GrowthTypeFast                 string
	ActionPropertyDifficultyLevel  string
	ActionPropertyDifficultyGrowth string
	ActionPropertyHTTPRequest      string
	Name                           string
	Enabled                        string
	OrgLevelOwner                  string
	OrgLevelInvited                string
	OrgLevelMember                 string
	MembersEndpoint                string
	ReportsEndpoint                string
	IntegrationsEndpoint           string
	EventsEndpoint                 string
	Tab                            string
	EditEndpoint                   string
	ConditionNegated               string
}

type RuleWizardRenderContext struct {
	CsrfRenderContext
	AlertRenderContext
	RuleFormData
	Countries  []CountryOption
	CurrentOrg *userOrg
	Property   *userProperty
	RuleID     string
	IsEdit     bool
}

var _ RenderContext = (*RuleWizardRenderContext)(nil)

func (c *RuleWizardRenderContext) Params() interface{} { return c }
func (c *RuleWizardRenderContext) Const() interface{}  { return ruleConstants }

func (c *RuleWizardRenderContext) parseUserAgentCondition() common.StatusCode {
	// Validate operator
	switch c.ConditionOperator {
	case string(dbgen.RuleConditionOperatorEquals),
		string(dbgen.RuleConditionOperatorContains),
		string(dbgen.RuleConditionOperatorEmpty):
	// Valid operators
	default:
		return common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if c.ConditionOperator != string(dbgen.RuleConditionOperatorEmpty) && c.ConditionValue == "" {
		return common.StatusRuleConditionValueRequired
	}

	return common.StatusOK
}

func (c *RuleWizardRenderContext) parseIPAddressCondition() common.StatusCode {
	// Validate operator - IP address can use matches or empty
	switch c.ConditionOperator {
	case string(dbgen.RuleConditionOperatorMatches),
		string(dbgen.RuleConditionOperatorEmpty):
	// Valid operators
	default:
		return common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if c.ConditionOperator != string(dbgen.RuleConditionOperatorEmpty) && c.ConditionValue == "" {
		return common.StatusRuleIPAddressRequired
	}

	return common.StatusOK
}

func (c *RuleWizardRenderContext) parseCountryCodeCondition() common.StatusCode {
	// Validate operator
	if c.ConditionOperator != string(dbgen.RuleConditionOperatorIn) {
		return common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if c.ConditionValue == "" {
		return common.StatusRuleCountryRequired
	}

	// Country codes are comma-separated
	return common.StatusOK
}

func (c *RuleWizardRenderContext) parseDifficultyAction() (int32, common.StatusCode) {
	if c.ActionValue == "" {
		return 0, common.StatusRuleActionValueRequired
	}

	val, err := strconv.ParseInt(c.ActionValue, 10, 32)
	if err != nil {
		return 0, common.StatusRuleActionValueInvalid
	}

	if val < -100 || val > 100 {
		return 0, common.StatusRuleDifficultyValueInvalid
	}

	return int32(val), common.StatusOK
}

func (c *RuleWizardRenderContext) parseHTTPRequestAction() (int32, common.StatusCode) {
	if len(c.ActionValue) > 0 {
		return 1, common.StatusOK
	}
	return 0, common.StatusOK
}

func (c *RuleWizardRenderContext) parseDifficultyGrowthAction() (int32, common.StatusCode) {
	if len(c.ActionValue) == 0 {
		return 0, common.StatusRuleActionValueRequired
	}

	val, err := strconv.ParseInt(c.ActionValue, 10, 32)
	if err != nil {
		return 0, common.StatusRuleActionValueInvalid
	}

	if val < 0 || val > 3 {
		return 0, common.StatusRuleDifficultyGrowthInvalid
	}

	return int32(val), common.StatusOK
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

		sort.Slice(cachedCountries, func(i, j int) bool {
			return strings.Compare(cachedCountries[i].Name, cachedCountries[j].Name) < 0
		})
	})

	return cachedCountries
}

func (s *Server) NewRuleWizardRenderContext(user *dbgen.User, org *dbgen.Organization, property *dbgen.Property) *RuleWizardRenderContext {
	renderCtx := &RuleWizardRenderContext{
		CsrfRenderContext: s.CreateCsrfContext(user),
		RuleFormData: RuleFormData{
			Enabled:           true,
			ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
			ConditionOperator: string(dbgen.RuleConditionOperatorEquals),
			ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		},
		Countries: getAllCountries(),
	}

	if property != nil {
		renderCtx.Property = propertyToUserProperty(property, s.IDHasher)
	}

	if org != nil {
		renderCtx.CurrentOrg = &userOrg{
			Name:  org.Name,
			ID:    s.IDHasher.Encrypt(int(org.ID)),
			Level: "",
		}
	}

	return renderCtx
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

	renderCtx := s.NewRuleWizardRenderContext(user, org, property)

	return &ViewModel{Model: renderCtx, View: ruleTemplate}, nil
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

	renderCtx := s.NewRuleWizardRenderContext(user, org, nil /*property*/)

	return &ViewModel{Model: renderCtx, View: ruleTemplate}, nil
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

	renderCtx := s.NewRuleWizardRenderContext(user, org, property)
	params, statusCode := s.parseRuleForm(ctx, r, renderCtx)

	if !statusCode.Success() {
		if len(renderCtx.ErrorMessage) == 0 {
			renderCtx.ErrorMessage = statusCode.String()
		}
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	params.PropertyID = db.Int(property.ID)

	_, auditEvent, err := s.Store.Impl().CreateDifficultyRule(ctx, user, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert difficulty rule", common.ErrAttr(err))
		renderCtx.ErrorMessage = common.StatusFailure.String()
		s.render(w, r, ruleFormTemplate, renderCtx)
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

	renderCtx := s.NewRuleWizardRenderContext(user, org, nil /*property*/)

	params, statusCode := s.parseRuleForm(ctx, r, renderCtx)

	if !statusCode.Success() {
		if len(renderCtx.ErrorMessage) == 0 {
			renderCtx.ErrorMessage = statusCode.String()
		}
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	params.OrgID = db.Int(org.ID)

	_, auditEvent, err := s.Store.Impl().CreateDifficultyRule(ctx, user, params)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to insert difficulty rule", common.ErrAttr(err))
		renderCtx.ErrorMessage = common.StatusFailure.String()
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	// Redirect back to rules tab with success message
	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)))+"?"+common.ParamTab+"="+common.RulesEndpoint, http.StatusOK, w, r)
}

func (s *Server) parseRuleForm(ctx context.Context, r *http.Request, renderCtx *RuleWizardRenderContext) (*dbgen.CreateDifficultyRuleParams, common.StatusCode) {
	renderCtx.Name = strings.TrimSpace(r.FormValue(common.ParamName))
	if len(renderCtx.Name) == 0 {
		renderCtx.NameError = common.StatusRuleNameEmptyError.String()
		return nil, common.StatusRuleNameEmptyError
	}

	_, renderCtx.Enabled = r.Form[common.ParamEnabled]

	renderCtx.ConditionProperty = r.FormValue(common.ParamConditionProperty)
	if len(renderCtx.ConditionProperty) == 0 {
		return nil, common.StatusRuleConditionPropertyRequired
	}

	renderCtx.ConditionOperator = r.FormValue(common.ParamConditionOperator)
	renderCtx.ConditionValue = strings.TrimSpace(r.FormValue(common.ParamConditionValue))

	// Parse operator for negation
	conditionNegated := false
	if strings.HasSuffix(renderCtx.ConditionOperator, "_negated") {
		renderCtx.ConditionNegated = true
		renderCtx.ConditionOperator = strings.TrimSuffix(renderCtx.ConditionOperator, "_negated")
	}

	// Validate and parse based on condition property type
	var conditionValueSeparator pgtype.Text
	var parseStatus common.StatusCode

	switch renderCtx.ConditionProperty {
	case string(dbgen.RuleConditionPropertyUserAgent):
		parseStatus = renderCtx.parseUserAgentCondition()
	case string(dbgen.RuleConditionPropertyIPAddress):
		parseStatus = renderCtx.parseIPAddressCondition()
	case string(dbgen.RuleConditionPropertyCountryCode):
		parseStatus = renderCtx.parseCountryCodeCondition()
		conditionValueSeparator = db.Text(",")
	default:
		slog.WarnContext(ctx, "Invalid condition property", "condition", renderCtx.ConditionProperty)
		return nil, common.StatusRuleConditionPropertyInvalid
	}

	if !parseStatus.Success() {
		slog.WarnContext(ctx, "Failed to parse rule condition", "condition", renderCtx.ConditionProperty, "operator", renderCtx.ConditionOperator,
			"value", renderCtx.ConditionValue, "negated", renderCtx.ConditionNegated, "status", parseStatus.String())
		return nil, parseStatus
	}

	renderCtx.ActionProperty = r.FormValue(common.ParamActionProperty)
	if len(renderCtx.ActionProperty) == 0 {
		slog.WarnContext(ctx, "Empty action property")
		return nil, common.StatusRuleActionPropertyRequired
	}

	renderCtx.ActionValue = strings.TrimSpace(r.FormValue(common.ParamActionValue))

	var actionValue int32
	var actionStatus common.StatusCode

	switch renderCtx.ActionProperty {
	case string(dbgen.RuleActionPropertyDifficultyLevelPercent):
		actionValue, actionStatus = renderCtx.parseDifficultyAction()
	case string(dbgen.RuleActionPropertyHTTPRequest):
		actionValue, actionStatus = renderCtx.parseHTTPRequestAction()
	case string(dbgen.RuleActionPropertyDifficultyGrowth):
		actionValue, actionStatus = renderCtx.parseDifficultyGrowthAction()
	default:
		slog.WarnContext(ctx, "Invalid action property", "action", renderCtx.ActionProperty)
		return nil, common.StatusRuleActionPropertyInvalid
	}

	if !actionStatus.Success() {
		slog.WarnContext(ctx, "Failed to parse rule action", "action", renderCtx.ActionProperty, "value", renderCtx.ActionValue,
			"status", actionStatus.String())
		return nil, actionStatus
	}

	params := &dbgen.CreateDifficultyRuleParams{
		Name:                     renderCtx.Name,
		Enabled:                  renderCtx.Enabled,
		ConditionProperty:        dbgen.RuleConditionProperty(renderCtx.ConditionProperty),
		ConditionOperator:        dbgen.RuleConditionOperator(renderCtx.ConditionOperator),
		ConditionOperatorNegated: conditionNegated,
		ConditionValueStr:        db.Text(renderCtx.ConditionValue),
		ConditionValueSeparator:  conditionValueSeparator,
		ActionProperty:           dbgen.RuleActionProperty(renderCtx.ActionProperty),
		ActionValue:              actionValue,
	}

	return params, common.StatusOK
}

func ruleToFormData(rule *dbgen.DifficultyRule) RuleFormData {
	conditionValue := ""
	if rule.ConditionValueStr.Valid {
		conditionValue = rule.ConditionValueStr.String
	}

	return RuleFormData{
		Name:              rule.Name,
		Enabled:           rule.Enabled,
		ConditionProperty: string(rule.ConditionProperty),
		ConditionOperator: string(rule.ConditionOperator),
		ConditionValue:    conditionValue,
		ActionProperty:    string(rule.ActionProperty),
		ActionValue:       strconv.Itoa(int(rule.ActionValue)),
		ConditionNegated:  rule.ConditionOperatorNegated,
	}
}

func (s *Server) ruleID(r *http.Request) (int32, string, error) {
	return common.IntPathArg(r, common.ParamRule, s.IDHasher)
}

func (s *Server) getPropertyEditRule(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
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

	ruleID, ruleIDStr, err := s.ruleID(r)
	if err != nil {
		return nil, err
	}

	rule, err := s.Store.Impl().RetrieveDifficultyRuleByProperty(ctx, ruleID, property.ID)
	if err != nil {
		return nil, err
	}

	renderCtx := s.NewRuleWizardRenderContext(user, org, property)
	renderCtx.RuleFormData = ruleToFormData(rule)
	renderCtx.RuleID = ruleIDStr
	renderCtx.IsEdit = true

	return &ViewModel{Model: renderCtx, View: ruleTemplate}, nil
}

func (s *Server) getOrgEditRule(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
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

	ruleID, ruleIDStr, err := s.ruleID(r)
	if err != nil {
		return nil, err
	}

	rule, err := s.Store.Impl().RetrieveDifficultyRuleByOrg(ctx, ruleID, org.ID)
	if err != nil {
		return nil, err
	}

	renderCtx := s.NewRuleWizardRenderContext(user, org, nil /*property*/)
	renderCtx.RuleFormData = ruleToFormData(rule)
	renderCtx.RuleID = ruleIDStr
	renderCtx.IsEdit = true

	return &ViewModel{Model: renderCtx, View: ruleTemplate}, nil
}

func (s *Server) postPropertyEditRule(w http.ResponseWriter, r *http.Request) {
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

	ruleID, ruleIDStr, err := s.ruleID(r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	renderCtx := s.NewRuleWizardRenderContext(user, org, property)
	renderCtx.RuleID = ruleIDStr
	renderCtx.IsEdit = true

	createParams, statusCode := s.parseRuleForm(ctx, r, renderCtx)

	if !statusCode.Success() {
		if len(renderCtx.ErrorMessage) == 0 {
			renderCtx.ErrorMessage = statusCode.String()
		}
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	updateParams := &dbgen.UpdateDifficultyRuleByPropertyParams{
		ID:                       ruleID,
		PropertyID:               db.Int(property.ID),
		Name:                     createParams.Name,
		Enabled:                  createParams.Enabled,
		ConditionProperty:        createParams.ConditionProperty,
		ConditionOperator:        createParams.ConditionOperator,
		ConditionOperatorNegated: createParams.ConditionOperatorNegated,
		ConditionValueStr:        createParams.ConditionValueStr,
		ConditionValueInt:        createParams.ConditionValueInt,
		ConditionValueSeparator:  createParams.ConditionValueSeparator,
		ActionProperty:           createParams.ActionProperty,
		ActionValue:              createParams.ActionValue,
	}

	_, auditEvent, err := s.Store.Impl().UpdateDifficultyRuleByProperty(ctx, user, updateParams)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update difficulty rule", common.ErrAttr(err))
		renderCtx.ErrorMessage = common.StatusFailure.String()
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertyEndpoint, s.IDHasher.Encrypt(int(property.ID)))+"?"+common.ParamTab+"="+common.RulesEndpoint, http.StatusOK, w, r)
}

func (s *Server) postOrgEditRule(w http.ResponseWriter, r *http.Request) {
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

	if !level.Valid || level.AccessLevel != dbgen.AccessLevelOwner {
		slog.WarnContext(ctx, "User is not org owner, cannot edit org rules", "orgID", org.ID, "userID", user.ID)
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	ruleID, ruleIDStr, err := s.ruleID(r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form", common.ErrAttr(err))
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	renderCtx := s.NewRuleWizardRenderContext(user, org, nil /*property*/)
	renderCtx.RuleID = ruleIDStr
	renderCtx.IsEdit = true

	createParams, statusCode := s.parseRuleForm(ctx, r, renderCtx)

	if !statusCode.Success() {
		if len(renderCtx.ErrorMessage) == 0 {
			renderCtx.ErrorMessage = statusCode.String()
		}
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	updateParams := &dbgen.UpdateDifficultyRuleByOrgParams{
		ID:                       ruleID,
		OrgID:                    db.Int(org.ID),
		Name:                     createParams.Name,
		Enabled:                  createParams.Enabled,
		ConditionProperty:        createParams.ConditionProperty,
		ConditionOperator:        createParams.ConditionOperator,
		ConditionOperatorNegated: createParams.ConditionOperatorNegated,
		ConditionValueStr:        createParams.ConditionValueStr,
		ConditionValueInt:        createParams.ConditionValueInt,
		ConditionValueSeparator:  createParams.ConditionValueSeparator,
		ActionProperty:           createParams.ActionProperty,
		ActionValue:              createParams.ActionValue,
	}

	_, auditEvent, err := s.Store.Impl().UpdateDifficultyRuleByOrg(ctx, user, updateParams)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update difficulty rule", common.ErrAttr(err))
		renderCtx.ErrorMessage = common.StatusFailure.String()
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)))+"?"+common.ParamTab+"="+common.RulesEndpoint, http.StatusOK, w, r)
}
