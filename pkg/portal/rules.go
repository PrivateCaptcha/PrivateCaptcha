package portal

import (
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type RuleConstants struct {
	BaseRenderConstants

	DashboardEndpoint              string
	OrgEndpoint                    string
	PropertyEndpoint               string
	RulesEndpoint                  string
	NewEndpoint                    string
	EditEndpoint                   string
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
	ConditionNegated               string
}

var (
	ruleConstants = RuleConstants{
		BaseRenderConstants:            baseConst,
		DashboardEndpoint:              common.DashboardEndpoint,
		OrgEndpoint:                    common.OrgEndpoint,
		PropertyEndpoint:               common.PropertyEndpoint,
		RulesEndpoint:                  common.RulesEndpoint,
		NewEndpoint:                    common.NewEndpoint,
		EditEndpoint:                   common.EditEndpoint,
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
		ConditionNegated:               common.ParamConditionNegated,
	}
)

// canEditRule checks if a user can edit a rule (org owner OR rule creator)
func canEditRule(user *dbgen.User, org *dbgen.Organization, rule *dbgen.DifficultyRule) bool {
	// Handle nil cases (e.g., from stub data)
	if user == nil || org == nil || rule == nil {
		return false
	}
	// Org owner can always edit
	if user.ID == org.UserID.Int32 {
		return true
	}
	// Rule creator can edit
	if rule.CreatorID.Valid && user.ID == rule.CreatorID.Int32 {
		return true
	}
	return false
}

// nolint:unused
func stubDifficultyRules() []*DifficultyRuleModel {
	hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "salt"))
	// For stub data, pass nil for user and org (CanEdit will be false)
	return []*DifficultyRuleModel{
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:                     "Block suspicious countries",
			Enabled:                  true,
			ConditionProperty:        dbgen.RuleConditionPropertyCountryCode,
			ConditionOperator:        dbgen.RuleConditionOperatorIn,
			ConditionOperatorNegated: false,
			ConditionValueStr:        db.Text("AB, CD, EF"),
			ConditionValueSeparator:  db.Text(","),
			Position:                 0,
			ActionProperty:           dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:              0,
			CreatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}, false, hasher),
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:                     "Block empty User-Agents",
			Enabled:                  false,
			ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator:        dbgen.RuleConditionOperatorEmpty,
			ConditionOperatorNegated: false,
			Position:                 1,
			ActionProperty:           dbgen.RuleActionPropertyHTTPRequest,
			ActionValue:              0,
			CreatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}, false, hasher),
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:                     "Lower difficulty for mobile",
			Enabled:                  true,
			ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator:        dbgen.RuleConditionOperatorContains,
			ConditionOperatorNegated: false,
			ConditionValueStr:        db.Text("Mobile"),
			Position:                 2,
			ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:              -20,
			CreatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}, false, hasher),
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:                     "Raise difficulty for crawlers",
			Enabled:                  true,
			ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator:        dbgen.RuleConditionOperatorContains,
			ConditionOperatorNegated: false,
			ConditionValueStr:        db.Text("curl"),
			Position:                 3,
			ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:              50,
			CreatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}, false, hasher),
		difficultyRuleToDisplay(&dbgen.DifficultyRule{
			Name:                     "Lower difficulty for trusted IPs",
			Enabled:                  true,
			ConditionProperty:        dbgen.RuleConditionPropertyIPAddress,
			ConditionOperator:        dbgen.RuleConditionOperatorMatches,
			ConditionOperatorNegated: false,
			ConditionValueStr:        db.Text("192.168.0.0/16"),
			Position:                 4,
			ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:              -30,
			CreatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
			UpdatedAt:                db.Timestampz(time.Now().Add(-2 * time.Hour)),
		}, false, hasher),
	}
}
