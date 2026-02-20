//go:build enterprise

package portal

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/biter777/countries"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	ruleTemplate     = "rules/rule.html"
	ruleFormTemplate = "rules/form.html"
)

func (s *Server) validateOrgRulesLimit(ctx context.Context, org *dbgen.Organization, user *dbgen.User) common.StatusCode {
	var subscr *dbgen.Subscription
	var err error

	// Get org owner
	owner := user
	if org.UserID.Valid && org.UserID.Int32 != user.ID {
		owner, err = s.Store.Impl().RetrieveUser(ctx, org.UserID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve org owner", "orgID", org.ID, common.ErrAttr(err))
			return common.StatusOK // Allow rule creation on error to avoid blocking legitimate use
		}
	}

	if owner.SubscriptionID.Valid {
		subscr, err = s.Store.Impl().RetrieveSubscription(ctx, owner.SubscriptionID.Int32)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to retrieve owner subscription", "userID", owner.ID, common.ErrAttr(err))
			return common.StatusOK // Allow rule creation on error to avoid blocking legitimate use
		}
	}

	ok, extra, err := s.SubscriptionLimits.CheckOrgRulesLimit(ctx, org.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return common.StatusOrgRulesSubscriptionRequired
		}
		slog.ErrorContext(ctx, "Failed to check org rules limit", "orgID", org.ID, common.ErrAttr(err))
		return common.StatusOK // Allow rule creation on error to avoid blocking legitimate use
	}

	if !ok {
		slog.WarnContext(ctx, "Org rules limit check failed", "extra", extra, "orgID", org.ID, "ownerID", owner.ID, "subscriptionID", subscr.ID,
			"internal", db.IsInternalSubscription(subscr.Source))

		return common.StatusOrgRulesLimitError
	}

	return common.StatusOK
}

func (s *Server) validatePropertyRulesLimit(ctx context.Context, org *dbgen.Organization, property *dbgen.Property, user *dbgen.User) common.StatusCode {
	// For properties, check limits of org owner (like validatePropertiesLimit)
	owner, subscr, err := s.Store.Impl().RetrieveOrgOwnerWithSubscription(ctx, org, user)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to retrieve org owner subscription", "orgID", org.ID, common.ErrAttr(err))
		return common.StatusOK // Allow rule creation on error to avoid blocking legitimate use
	}

	ok, extra, err := s.SubscriptionLimits.CheckPropertyRulesLimit(ctx, property.ID, subscr)
	if err != nil {
		if err == db.ErrNoActiveSubscription {
			return common.StatusPropertyRulesSubscriptionRequired
		}
		slog.ErrorContext(ctx, "Failed to check property rules limit", "propertyID", property.ID, common.ErrAttr(err))
		return common.StatusOK // Allow rule creation on error to avoid blocking legitimate use
	}

	if !ok {
		slog.WarnContext(ctx, "Property rules limit check failed", "extra", extra, "propertyID", property.ID, "ownerID", owner.ID, "subscriptionID", subscr.ID,
			"internal", db.IsInternalSubscription(subscr.Source))

		return common.StatusPropertyRulesLimitError
	}

	return common.StatusOK
}

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

func (c *RuleWizardRenderContext) parseUserAgentCondition() common.StatusCode {
	// Validate operator
	switch c.ConditionOperator {
	case string(dbgen.RuleConditionOperatorEquals),
		string(dbgen.RuleConditionOperatorContains),
		string(dbgen.RuleConditionOperatorEmpty),
		string(dbgen.RuleConditionOperatorBot):
	// Valid operators
	default:
		return common.StatusRuleConditionOperatorInvalid
	}

	// Validate value (bot and empty operators don't require a value)
	if c.ConditionOperator != string(dbgen.RuleConditionOperatorEmpty) &&
		c.ConditionOperator != string(dbgen.RuleConditionOperatorBot) &&
		c.ConditionValue == "" {
		return common.StatusRuleConditionValueRequired
	}

	return common.StatusOK
}

func (c *RuleWizardRenderContext) parseIPAddressCondition(separator string) common.StatusCode {
	// Validate operator - IP address can use matches or empty
	switch c.ConditionOperator {
	case string(dbgen.RuleConditionOperatorMatches),
		string(dbgen.RuleConditionOperatorEmpty):
	// Valid operators
	default:
		return common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if c.ConditionOperator != string(dbgen.RuleConditionOperatorEmpty) {
		if c.ConditionValue == "" {
			return common.StatusRuleIPAddressRequired
		}
		items := strings.Split(c.ConditionValue, separator)
		if len(items) > rules.MaxIPAddressValues {
			return common.StatusRuleIPAddressTooMany
		}
		for _, item := range items {
			item = strings.TrimSpace(item)
			if len(item) == 0 {
				return common.StatusRuleIPAddressInvalid
			}
			_, err := netip.ParsePrefix(item)
			if err != nil {
				_, addrErr := netip.ParseAddr(item)
				if addrErr != nil {
					return common.StatusRuleIPAddressInvalid
				}
			}
		}
	}

	return common.StatusOK
}

func (c *RuleWizardRenderContext) parseCountryCodeCondition(separator string) common.StatusCode {
	// Validate operator
	if c.ConditionOperator != string(dbgen.RuleConditionOperatorIn) {
		return common.StatusRuleConditionOperatorInvalid
	}

	// Validate value
	if c.ConditionValue == "" {
		return common.StatusRuleCountryRequired
	}

	values := strings.Split(c.ConditionValue, separator)
	for _, cc := range values {
		data := countries.ByName(cc)
		if data == countries.Unknown {
			return common.StatusRuleCountryInvalid
		}
	}

	// Country codes are comma-separated
	return common.StatusOK
}

func (c *RuleWizardRenderContext) parseDomainCondition(domain string) common.StatusCode {
	if len(domain) == 0 {
		// not supported for orgs (that pass empty domain)
		return common.StatusRuleConditionPropertyInvalid
	}

	// Validate operator
	switch c.ConditionOperator {
	case string(dbgen.RuleConditionOperatorEquals),
		string(dbgen.RuleConditionOperatorContains),
		string(dbgen.RuleConditionOperatorEmpty):
	// Valid operators
	default:
		return common.StatusRuleConditionOperatorInvalid
	}

	if c.ConditionOperator != string(dbgen.RuleConditionOperatorEmpty) {
		if c.ConditionValue == "" {
			return common.StatusRuleDomainRequired
		}
		parsedDomain, err := common.ParseDomainName(c.ConditionValue)
		if err != nil {
			return common.StatusRuleDomainInvalid
		}
		if !common.IsSubDomainOrDomain(parsedDomain, domain) {
			return common.StatusRuleDomainSubdomain
		}
		c.ConditionValue = parsedDomain
	}

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
	params, statusCode := s.parseRuleForm(ctx, r, renderCtx, property.Domain)

	if !statusCode.Success() {
		if len(renderCtx.ErrorMessage) == 0 {
			renderCtx.ErrorMessage = statusCode.String()
		}
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	if limitStatus := s.validatePropertyRulesLimit(ctx, org, property, user); !limitStatus.Success() {
		renderCtx.ErrorMessage = limitStatus.String()
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	params.PropertyID = db.Int(property.ID)
	params.CreatorID = db.Int(user.ID)

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
	params, statusCode := s.parseRuleForm(ctx, r, renderCtx, "" /*domain*/)

	if !statusCode.Success() {
		if len(renderCtx.ErrorMessage) == 0 {
			renderCtx.ErrorMessage = statusCode.String()
		}
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	if limitStatus := s.validateOrgRulesLimit(ctx, org, user); !limitStatus.Success() {
		renderCtx.ErrorMessage = limitStatus.String()
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	params.OrgID = db.Int(org.ID)
	params.CreatorID = db.Int(user.ID)

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

func (s *Server) parseRuleForm(ctx context.Context, r *http.Request, renderCtx *RuleWizardRenderContext, domain string) (*dbgen.CreateDifficultyRuleParams, common.StatusCode) {
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
		conditionValueSeparator = db.Text(",")
		parseStatus = renderCtx.parseIPAddressCondition(conditionValueSeparator.String)
	case string(dbgen.RuleConditionPropertyCountryCode):
		conditionValueSeparator = db.Text(",")
		parseStatus = renderCtx.parseCountryCodeCondition(conditionValueSeparator.String)
	case string(dbgen.RuleConditionPropertyDomain):
		parseStatus = renderCtx.parseDomainCondition(domain)
	default:
		slog.WarnContext(ctx, "Invalid condition property", "condition", renderCtx.ConditionProperty)
		return nil, common.StatusRuleConditionPropertyInvalid
	}

	if !parseStatus.Success() {
		slog.WarnContext(ctx, "Failed to parse rule condition", "condition", renderCtx.ConditionProperty, "operator", renderCtx.ConditionOperator,
			"value", renderCtx.ConditionValue, "negated", renderCtx.ConditionNegated, "status", parseStatus.String())
		return nil, parseStatus
	}

	slog.DebugContext(ctx, "Parsed rule condition", "condition", renderCtx.ConditionProperty, "operator", renderCtx.ConditionOperator,
		"value", renderCtx.ConditionValue, "negated", renderCtx.ConditionNegated)

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

	slog.DebugContext(ctx, "Parsed rule action", "action", renderCtx.ActionProperty, "value", renderCtx.ActionValue)

	params := &dbgen.CreateDifficultyRuleParams{
		Name:                     renderCtx.Name,
		Enabled:                  renderCtx.Enabled,
		ConditionProperty:        dbgen.RuleConditionProperty(renderCtx.ConditionProperty),
		ConditionOperator:        dbgen.RuleConditionOperator(renderCtx.ConditionOperator),
		ConditionOperatorNegated: renderCtx.ConditionNegated,
		ConditionValueStr:        db.Text(renderCtx.ConditionValue),
		ConditionValueSeparator:  conditionValueSeparator,
		ActionProperty:           dbgen.RuleActionProperty(renderCtx.ActionProperty),
		ActionValue:              actionValue,
	}

	return params, common.StatusOK
}

const errUpdateRuleMessage = "Failed to update the difficulty rule. Please try again later."

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

func (s *Server) Rule(r *http.Request) (*dbgen.DifficultyRule, error) {
	ctx := r.Context()

	ruleID, value, err := common.IntPathArg(r, common.ParamRule, s.IDHasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse rule path parameter", "value", value, common.ErrAttr(err))
		return nil, errInvalidPathArg
	}

	rule, err := s.Store.Impl().RetrieveDifficultyRule(ctx, int32(ruleID))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find rule by ID", "ruleID", ruleID, common.ErrAttr(err))
		return nil, err
	}

	return rule, nil
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

	rule, err := s.Rule(r)
	if err != nil {
		return nil, err
	}

	// Check if user can edit this rule (org owner OR rule creator)
	if !canEditRule(user, org, rule) {
		slog.WarnContext(ctx, "User cannot edit rule", "userID", user.ID, "ruleID", rule.ID, "orgOwnerID", org.UserID.Int32, "ruleCreatorID", rule.CreatorID)
		return nil, db.ErrPermissions
	}

	renderCtx := s.NewRuleWizardRenderContext(user, org, property)
	renderCtx.RuleFormData = ruleToFormData(rule)
	renderCtx.RuleID = s.IDHasher.Encrypt(int(rule.ID))
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

	rule, err := s.Rule(r)
	if err != nil {
		return nil, err
	}

	// Check if user can edit this rule (org owner OR rule creator)
	if !canEditRule(user, org, rule) {
		slog.WarnContext(ctx, "User cannot edit org rule", "userID", user.ID, "ruleID", rule.ID, "orgOwnerID", org.UserID.Int32, "ruleCreatorID", rule.CreatorID)
		return nil, db.ErrPermissions
	}

	renderCtx := s.NewRuleWizardRenderContext(user, org, nil /*property*/)
	renderCtx.RuleFormData = ruleToFormData(rule)
	renderCtx.RuleID = s.IDHasher.Encrypt(int(rule.ID))
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

	rule, err := s.Rule(r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	// Check if user can edit this rule (org owner OR rule creator)
	if !canEditRule(user, org, rule) {
		slog.WarnContext(ctx, "User cannot edit property rule", "userID", user.ID, "ruleID", rule.ID, "orgOwnerID", org.UserID.Int32, "ruleCreatorID", rule.CreatorID)
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
	renderCtx.RuleID = s.IDHasher.Encrypt(int(rule.ID))
	renderCtx.IsEdit = true

	createParams, statusCode := s.parseRuleForm(ctx, r, renderCtx, property.Domain)

	if !statusCode.Success() {
		if len(renderCtx.ErrorMessage) == 0 {
			renderCtx.ErrorMessage = statusCode.String()
		}
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	updateParams := &dbgen.UpdateDifficultyRuleParams{
		ID:                       rule.ID,
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

	_, auditEvent, err := s.Store.Impl().UpdateDifficultyRule(ctx, org, property, user, updateParams)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update difficulty rule", "ruleID", rule.ID, "propertyID", property.ID, common.ErrAttr(err))
		renderCtx.ErrorMessage = errUpdateRuleMessage
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

	// Check org membership
	if !level.Valid || level.AccessLevel == dbgen.AccessLevelInvited {
		slog.WarnContext(ctx, "User is not org member", "orgID", org.ID, "userID", user.ID)
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	rule, err := s.Rule(r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	// Check if user can edit this rule (org owner OR rule creator)
	if !canEditRule(user, org, rule) {
		slog.WarnContext(ctx, "User cannot edit org rule", "userID", user.ID, "ruleID", rule.ID, "orgOwnerID", org.UserID.Int32, "ruleCreatorID", rule.CreatorID)
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
	renderCtx.RuleID = s.IDHasher.Encrypt(int(rule.ID))
	renderCtx.IsEdit = true

	createParams, statusCode := s.parseRuleForm(ctx, r, renderCtx, "" /*domain*/)

	if !statusCode.Success() {
		if len(renderCtx.ErrorMessage) == 0 {
			renderCtx.ErrorMessage = statusCode.String()
		}
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	updateParams := &dbgen.UpdateDifficultyRuleParams{
		ID:                       rule.ID,
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

	_, auditEvent, err := s.Store.Impl().UpdateDifficultyRule(ctx, org, nil, user, updateParams)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update difficulty rule", "ruleID", rule.ID, "orgID", org.ID, common.ErrAttr(err))
		renderCtx.ErrorMessage = errUpdateRuleMessage
		s.render(w, r, ruleFormTemplate, renderCtx)
		return
	}

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)))+"?"+common.ParamTab+"="+common.RulesEndpoint, http.StatusOK, w, r)
}

func (s *Server) deletePropertyRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	property, err := s.Property(org, r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	rule, err := s.Rule(r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	// Check if user can delete this rule (org owner OR rule creator)
	if !canEditRule(user, org, rule) {
		slog.WarnContext(ctx, "User cannot delete property rule", "userID", user.ID, "ruleID", rule.ID, "orgOwnerID", org.UserID.Int32, "ruleCreatorID", rule.CreatorID)
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	auditEvent, err := s.Store.Impl().DeleteDifficultyRule(ctx, rule, user)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete difficulty rule", "ruleID", rule.ID, "propertyID", property.ID, common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)), common.PropertyEndpoint, s.IDHasher.Encrypt(int(property.ID)))+"?"+common.ParamTab+"="+common.RulesEndpoint, http.StatusOK, w, r)
}

func (s *Server) deleteOrgRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		s.RedirectError(http.StatusUnauthorized, w, r)
		return
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	rule, err := s.Rule(r)
	if err != nil {
		s.RedirectError(http.StatusBadRequest, w, r)
		return
	}

	// Check if user can delete this rule (org owner OR rule creator)
	if !canEditRule(user, org, rule) {
		slog.WarnContext(ctx, "User cannot delete org rule", "userID", user.ID, "ruleID", rule.ID, "orgOwnerID", org.UserID.Int32, "ruleCreatorID", rule.CreatorID)
		s.RedirectError(http.StatusForbidden, w, r)
		return
	}

	auditEvent, err := s.Store.Impl().DeleteDifficultyRule(ctx, rule, user)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to delete difficulty rule", "ruleID", rule.ID, "orgID", org.ID, common.ErrAttr(err))
		s.RedirectError(http.StatusInternalServerError, w, r)
		return
	}

	s.Store.AuditLog().RecordEvent(ctx, auditEvent, common.AuditLogSourcePortal)

	common.Redirect(s.PartsURL(common.OrgEndpoint, s.IDHasher.Encrypt(int(org.ID)))+"?"+common.ParamTab+"="+common.RulesEndpoint, http.StatusOK, w, r)
}

func (s *Server) postMovePropertyRule(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
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

	rule, err := s.Rule(r)
	if err != nil {
		return nil, err
	}

	// Check if user can move this rule (org owner OR rule creator)
	if !canEditRule(user, org, rule) {
		slog.WarnContext(ctx, "User cannot move property rule", "userID", user.ID, "ruleID", rule.ID, "orgOwnerID", org.UserID.Int32, "ruleCreatorID", rule.CreatorID)
		return nil, db.ErrPermissions
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form", common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}

	// Parse new position index
	positionStr := r.FormValue(common.ParamPosition)
	newIndex, err := strconv.Atoi(positionStr)
	if err != nil {
		slog.ErrorContext(ctx, "Invalid position value", "position", positionStr, common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}

	if newIndex < 0 {
		slog.ErrorContext(ctx, "Invalid position value", "index", newIndex)
		return nil, ErrInvalidRequestArg
	}

	if existingRules, err := s.Store.Impl().GetCachedPropertyRules(ctx, property.ID); err == nil {
		if newIndex >= len(existingRules) {
			slog.ErrorContext(ctx, "Invalid position value", "index", newIndex, "count", len(existingRules))
			return nil, ErrInvalidRequestArg
		}
	}

	var auditEvent *common.AuditLogEvent
	if _, err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		var moveErr error
		_, auditEvent, moveErr = impl.MoveDifficultyRuleWithRebalancing(ctx, rule, newIndex, user)
		if moveErr != nil {
			return nil, moveErr
		}
		return []*common.AuditLogEvent{auditEvent}, nil
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to move rule", "ruleID", rule.ID, "propertyID", property.ID, common.ErrAttr(err))
		return nil, err
	}

	renderCtx, _, err := s.getPropertyRules(w, r)
	if err != nil {
		return nil, err
	}

	return &ViewModel{Model: renderCtx, View: propertyDashboardRulesTemplate, AuditEvent: auditEvent}, nil
}

func (s *Server) postMoveOrgRule(w http.ResponseWriter, r *http.Request) (*ViewModel, error) {
	ctx := r.Context()

	user, err := s.SessionUser(ctx, s.Session(w, r))
	if err != nil {
		return nil, err
	}

	org, _, err := s.Org(user, r)
	if err != nil {
		return nil, err
	}

	rule, err := s.Rule(r)
	if err != nil {
		return nil, err
	}

	// Check if user can move this rule (org owner OR rule creator)
	if !canEditRule(user, org, rule) {
		slog.WarnContext(ctx, "User cannot move org rule", "userID", user.ID, "ruleID", rule.ID, "orgOwnerID", org.UserID.Int32, "ruleCreatorID", rule.CreatorID)
		return nil, db.ErrPermissions
	}

	err = r.ParseForm()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse form", common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}

	// Parse new position index
	positionStr := r.FormValue(common.ParamPosition)
	newIndex, err := strconv.Atoi(positionStr)
	if err != nil {
		slog.ErrorContext(ctx, "Invalid position value", "position", positionStr, common.ErrAttr(err))
		return nil, ErrInvalidRequestArg
	}

	if newIndex < 0 {
		slog.ErrorContext(ctx, "Invalid position value", "index", newIndex)
		return nil, ErrInvalidRequestArg
	}

	if existingRules, err := s.Store.Impl().GetCachedOrgRules(ctx, org.ID); err == nil {
		if newIndex >= len(existingRules) {
			slog.ErrorContext(ctx, "Invalid position value", "index", newIndex, "count", len(existingRules))
			return nil, ErrInvalidRequestArg
		}
	}

	var auditEvent *common.AuditLogEvent
	if _, err := s.Store.WithTx(ctx, func(impl *db.BusinessStoreImpl) ([]*common.AuditLogEvent, error) {
		var moveErr error
		_, auditEvent, moveErr = impl.MoveDifficultyRuleWithRebalancing(ctx, rule, newIndex, user)
		if moveErr != nil {
			return nil, moveErr
		}
		return []*common.AuditLogEvent{auditEvent}, nil
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to move rule", "ruleID", rule.ID, "orgID", org.ID, common.ErrAttr(err))
		return nil, err
	}

	renderCtx, _, err := s.createOrgRulesContext(ctx, org, user)
	if err != nil {
		return nil, err
	}

	return &ViewModel{
		Model:      renderCtx,
		View:       orgRulesTemplate,
		AuditEvent: auditEvent,
	}, nil
}
