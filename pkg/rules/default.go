package rules

import (
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
)

func DefaultDifficultyRule() *dbgen.DifficultyRule {
	return &dbgen.DifficultyRule{
		ID:                0, // Excluded from rule statistics.
		Name:              "Default automation protection (no organization rules)",
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyClientSideDetection,
		ConditionOperator: dbgen.RuleConditionOperatorIn,
		ConditionValueStr: pgtype.Text{
			String: "automation",
			Valid:  true,
		},
		ActionProperty: dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:    300,
		Terminal:       false,
	}
}
