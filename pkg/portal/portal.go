package portal

import (
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type portalRenderContext struct {
	UserName string
}

func (s *Server) portal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sess := s.Session.SessionStart(w, r)
	email, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok || (len(email) == 0) {
		slog.ErrorContext(ctx, "Failed to get user email from context")
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	user, err := s.Store.FindUser(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to find user by email", common.ErrAttr(err))
		s.htmxRedirectError(http.StatusInternalServerError, w, r)
		return
	}

	renderCtx := &portalRenderContext{
		UserName: user.UserName,
	}

	s.render(r.Context(), w, "portal/portal.html", renderCtx)
}
