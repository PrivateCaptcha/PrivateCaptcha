//go:build !enterprise

package portal

import (
	"net/http"

	"github.com/justinas/alice"
)

func (s *Server) isEnterprise() bool {
	return false
}

func (s *Server) setupEnterprise(*http.ServeMux, *RouteGenerator, alice.Chain) {
	// BUMP
}
