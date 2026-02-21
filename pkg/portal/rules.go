package portal

import (
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

// ConditionFormParser validates and parses a rule condition from form data.
// conditionOperator and conditionValue come from the submitted form.
// domain is the property domain (empty for org-level rules).
// Returns the normalized condition value, separator (empty means none), and parse status.
type ConditionFormParser func(conditionOperator, conditionValue, domain string) (normalizedValue, separator string, status common.StatusCode)

// ActionFormParser validates and parses a rule action from form data.
// actionValue comes from the submitted form.
// Returns the integer action value and parse status.
type ActionFormParser func(actionValue string) (int32, common.StatusCode)

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
