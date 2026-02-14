package portal

import (
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

// nolint:unused
func stubDifficultyRules() []*DifficultyRuleModel {
	stubRules := []*dbgen.DifficultyRule{
		{
			ID:                       1,
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
		},
		{
			ID:                       2,
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
		},
		{
			ID:                       3,
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
		},
		{
			ID:                       4,
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
		},
		{
			ID:                       5,
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
		},
	}

	models := make([]*DifficultyRuleModel, 0, len(stubRules))
	for _, rule := range stubRules {
		model := difficultyRuleToDisplay(rule, stubHasher{})
		models = append(models, model)
	}
	return models
}

// nolint:unused
type stubHasher struct{}

// nolint:unused
func (s stubHasher) Encrypt(id int) string { return strconv.Itoa(id) }

// nolint:unused
func (s stubHasher) Encrypt64(id int64) string { return strconv.FormatInt(id, 10) }

// nolint:unused
func (s stubHasher) Decrypt(id string) (int, error) { return strconv.Atoi(id) }

// nolint:unused
func (s stubHasher) Decrypt64(id string) (int64, error) { return strconv.ParseInt(id, 10, 64) }
