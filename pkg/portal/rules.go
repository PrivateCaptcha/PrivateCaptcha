package portal

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	errRuleConditionPropertyEmpty = errors.New("rule condition property is empty")
	errRuleActionEmpty            = errors.New("rule action is empty")
	titleCaser                    = cases.Title(language.Und)
)

type rulesRenderContext struct {
	Rules     []*DifficultyRuleModel
	CanAddNew bool
}

type DifficultyRuleModel struct {
	ID                string
	Name              string
	ConditionProperty string
	ConditionOperator string
	ConditionValue    string
	ActionAction      string
	ActionProperty    string
	ActionValue       string
	Enabled           bool
	CanEdit           bool
	Terminal          bool
}

func difficultyRuleToDisplay(rule *dbgen.DifficultyRule, canEdit bool, hasher common.IdentifierHasher, registry *RuleRegistry) *DifficultyRuleModel {
	if rule == nil {
		return &DifficultyRuleModel{CanEdit: canEdit}
	}

	conditionValue := registry.FormatConditionValue(rule)

	var actionProperty string
	var actionValue string
	var actionAction string

	switch rule.ActionProperty {
	case dbgen.RuleActionPropertyHTTPRequest:
		actionAction = "block"
		actionProperty = "HTTP request"
	case dbgen.RuleActionPropertyDifficultyLevelPercent:
		actionAction = "change"
		actionProperty = "Difficulty level"
		// Format percentage with +/- sign
		if percent := rule.ActionValue; percent >= 0 {
			actionValue = fmt.Sprintf("+%d%%", percent)
		} else {
			actionValue = fmt.Sprintf("%d%%", percent)
		}
	case dbgen.RuleActionPropertyDifficultyGrowth:
		actionProperty = "Difficulty growth"
		actionAction = "set"
		actionValue = string(growthLevelFromIndex(int(rule.ActionValue)))
	case dbgen.RuleActionPropertyBreak:
		actionAction = "stop"
		actionProperty = "processing rules"
	default:
		actionProperty = titleCaser.String(strings.ReplaceAll(string(rule.ActionProperty), "_", " "))
		actionValue = fmt.Sprintf("%d", rule.ActionValue)
		actionAction = "set"
	}

	var conditionOperator string
	switch rule.ConditionOperator {
	case dbgen.RuleConditionOperatorEmpty:
		if rule.ConditionOperatorNegated {
			conditionOperator = "is not empty"
		} else {
			conditionOperator = "is empty"
		}
	case dbgen.RuleConditionOperatorIn:
		if rule.ConditionOperatorNegated {
			conditionOperator = "is not one of"
		} else {
			conditionOperator = "is one of"
		}
	case dbgen.RuleConditionOperatorBot:
		if rule.ConditionOperatorNegated {
			conditionOperator = "is not known bot"
		} else {
			conditionOperator = "is known bot"
		}
	default:
		baseOperator := strings.ReplaceAll(string(rule.ConditionOperator), "_", " ")
		if rule.ConditionOperatorNegated {
			conditionOperator = "not " + baseOperator
		} else {
			conditionOperator = baseOperator
		}
	}

	var conditionProperty string
	if registry != nil {
		conditionProperty = registry.ConditionDisplayName(string(rule.ConditionProperty))
	} else {
		conditionProperty = titleCaser.String(strings.ReplaceAll(string(rule.ConditionProperty), "_", " "))
	}

	return &DifficultyRuleModel{
		ID:                hasher.Encrypt(int(rule.ID)),
		Name:              rule.Name,
		Enabled:           rule.Enabled,
		ConditionProperty: conditionProperty,
		ConditionOperator: conditionOperator,
		ConditionValue:    conditionValue,
		ActionAction:      actionAction,
		ActionProperty:    actionProperty,
		ActionValue:       actionValue,
		CanEdit:           canEdit,
		Terminal:          rule.Terminal,
	}
}

type ConditionFormParser func(conditionOperator, conditionValue, domain string) (normalizedValue, separator string, status common.StatusCode)
type ActionFormParser func(actionValue string) (int32, common.StatusCode)
type ConditionValueFormatter func(rule *dbgen.DifficultyRule) string

type ConditionRegistration struct {
	Parser         ConditionFormParser
	DisplayName    string
	ValueFormatter ConditionValueFormatter
}

type RuleRegistry struct {
	conditions map[string]ConditionRegistration
	actions    map[string]ActionFormParser
}

func (r *RuleRegistry) RegisterCondition(key string, parser ConditionFormParser, displayName string, valueFormatter ConditionValueFormatter) error {
	if len(key) == 0 {
		return errRuleConditionPropertyEmpty
	}

	reg := ConditionRegistration{Parser: parser, DisplayName: displayName}
	if valueFormatter != nil {
		reg.ValueFormatter = valueFormatter
	}
	r.conditions[key] = reg

	return nil
}

func (r *RuleRegistry) RegisterAction(key string, parser ActionFormParser) error {
	if len(key) == 0 {
		return errRuleActionEmpty
	}

	r.actions[key] = parser

	return nil
}

func (r *RuleRegistry) ConditionParser(key string) (ConditionFormParser, bool) {
	reg, ok := r.conditions[key]
	if !ok {
		return nil, false
	}
	return reg.Parser, true
}

func (r *RuleRegistry) ActionParser(key string) (ActionFormParser, bool) {
	parser, ok := r.actions[key]
	return parser, ok
}

func (r *RuleRegistry) ConditionDisplayName(key string) string {
	reg, ok := r.conditions[key]
	if ok && (len(reg.DisplayName) > 0) {
		return reg.DisplayName
	}

	return titleCaser.String(strings.ReplaceAll(key, "_", " "))
}

func (r *RuleRegistry) FormatConditionValue(rule *dbgen.DifficultyRule) string {
	if r != nil {
		reg, ok := r.conditions[string(rule.ConditionProperty)]
		if ok && reg.ValueFormatter != nil {
			return reg.ValueFormatter(rule)
		}
	}

	if rule.ConditionValueStr.Valid {
		return rule.ConditionValueStr.String
	}
	if rule.ConditionValueInt.Valid {
		return fmt.Sprintf("%d", rule.ConditionValueInt.Int32)
	}
	return ""
}

// canEditRule checks if a user can edit a rule (org owner OR rule creator)
func canEditRule(user *dbgen.User, org *dbgen.Organization, rule *dbgen.DifficultyRule) bool {
	// Handle nil cases (e.g., from stub data)
	if user == nil || org == nil || rule == nil {
		return false
	}
	// Org owner can always edit
	if org.UserID.Valid && (user.ID == org.UserID.Int32) {
		return true
	}
	// Rule creator can edit
	if rule.CreatorID.Valid && user.ID == rule.CreatorID.Int32 {
		return true
	}
	return false
}

// nolint:unused
func StubDifficultyRules() []*DifficultyRuleModel {
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
		}, false, hasher, nil),
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
		}, false, hasher, nil),
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
		}, false, hasher, nil),
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
		}, false, hasher, nil),
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
		}, false, hasher, nil),
	}
}
