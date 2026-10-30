//go:build !enterprise

package api

import (
	"context"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/justinas/alice"
)

func (s *Server) initSystemRules(ctx context.Context) {
	s.SystemRules = rules.NewRulesCompiler(nil /*ua parser*/).Compile(ctx, []*dbgen.DifficultyRule{
		{
			ID:                0, // Excluded from rule statistics
			Name:              "Client-side automation detection",
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
		},
		&dbgen.DifficultyRule{
			ID:                0, // Excluded from rule statistics
			Name:              "User-Agent bot detection",
			Enabled:           true,
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator: dbgen.RuleConditionOperatorBot,
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       300,
			Terminal:          false,
		},
	})
}

func (s *Server) setupEnterprise(rg *common.RouteGenerator, publicChain alice.Chain, apiRateLimiter func(next http.Handler) http.Handler) {
}

func (s *Server) RegisterTaskHandlers(ctx context.Context) {
	// BUMP
}

func (s *Server) retrievePropertyRules(ctx context.Context, property *dbgen.Property) *rules.RulesPair {
	return &rules.RulesPair{
		SystemRules: s.SystemRules,
	}
}

func (am *AuthMiddleware) RefreshPropertyRules(ctx context.Context, propertyID int32) {
}
