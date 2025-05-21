//go:build !enterprise

package portal

import (
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func (s *Server) isEnterprise() bool {
	return false
}

func (s *Server) enterprise(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Log(r.Context(), common.LevelTrace, "Rejecting request for enterprise")
		http.Error(w, "", http.StatusForbidden)
	})
}
