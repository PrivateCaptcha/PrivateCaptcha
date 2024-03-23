package portal

import "net/http"

func (s *Server) portal(w http.ResponseWriter, r *http.Request) {
	s.render(r.Context(), w, "portal/portal.html", struct{}{})
}
