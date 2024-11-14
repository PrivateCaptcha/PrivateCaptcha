package portal

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/justinas/alice"
)

func (s *Server) createCsrfContext(user *dbgen.User) csrfRenderContext {
	return csrfRenderContext{
		Token: s.XSRF.Token(strconv.Itoa(int(user.ID))),
	}
}

func (s *Server) csrfUserEmailKeyFunc(w http.ResponseWriter, r *http.Request) string {
	sess := s.Session.SessionStart(w, r)
	userEmail, ok := sess.Get(session.KeyUserEmail).(string)
	if !ok {
		slog.WarnContext(r.Context(), "Session does not contain a valid email")
	}

	return userEmail
}

func (s *Server) csrfUserIDKeyFunc(w http.ResponseWriter, r *http.Request) string {
	sess := s.Session.SessionStart(w, r)
	userID, ok := sess.Get(session.KeyUserID).(int32)
	if !ok {
		slog.WarnContext(r.Context(), "Session does not contain a valid userID")
		return ""
	}

	return strconv.Itoa(int(userID))
}

func (s *Server) csrf(keyFunc CsrfKeyFunc) alice.Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
				next.ServeHTTP(w, r)
				return
			}

			token := r.Header.Get(common.HeaderCSRFToken)
			if len(token) == 0 {
				token = r.FormValue(common.ParamCSRFToken)
			}

			if len(token) > 0 {
				userID := keyFunc(w, r)
				if s.XSRF.VerifyToken(token, userID) {
					next.ServeHTTP(w, r)
					return
				} else {
					slog.WarnContext(ctx, "Failed to verify CSRF token")
				}
			} else {
				slog.WarnContext(ctx, "CSRF token is missing")
			}

			common.Redirect(s.relURL(common.ExpiredEndpoint), w, r)
		})
	}
}
