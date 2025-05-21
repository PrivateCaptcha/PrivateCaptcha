//go:build enterprise

package portal

import "net/http"

func (s *Server) isEnterprise() bool {
	return true
}

func (s *Server) enterprise(next http.Handler) http.Handler {
	return next
}
