package portal

import (
	"net/http"
)

func (s *Server) portal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := s.Session.SessionStart(w, r)
	renderCtx, err := s.createOrgDashboardContext(ctx, -1, sess)
	if err != nil {
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	s.render(w, r, "portal/portal.html", renderCtx)
}
