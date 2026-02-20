//go:build !enterprise

package api

import (
	"context"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/justinas/alice"
)

func (s *Server) setupEnterprise(rg *common.RouteGenerator, publicChain alice.Chain, apiRateLimiter func(next http.Handler) http.Handler) {
}

func (s *Server) RegisterTaskHandlers(ctx context.Context) {
	// BUMP
}

func (s *Server) retrievePropertyRules(ctx context.Context, property *dbgen.Property) *rules.RulesPair {
	return nil
}

func (am *AuthMiddleware) RefreshPropertyRules(ctx context.Context, propertyID int32) {
}
