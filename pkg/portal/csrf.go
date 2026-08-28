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

func (s *Server) CreateCsrfContext(user *dbgen.User) CsrfRenderContext {
	return CsrfRenderContext{
		Token: s.XSRF.Token(strconv.Itoa(int(user.ID))),
	}
}

func (s *Server) csrfSessionIDKeyFunc(w http.ResponseWriter, r *http.Request) string {
	sid, _ := s.Sessions.SessionID(r)
	return sid
}

func (s *Server) csrfUserIDKeyFunc(w http.ResponseWriter, r *http.Request) string {
	sess, ok := r.Context().Value(common.SessionContextKey).(*session.Session)
	if !ok {
		// Public challenge routes do not have the private middleware context.
		sess, ok = s.Sessions.SessionGet(r)
	}
	if !ok {
		return ""
	}

	ctx := r.Context()
	userID, ok := sess.Get(ctx, session.KeyUserID).(int32)
	if !ok {
		slog.WarnContext(ctx, "Session does not contain a valid userID")
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
					slog.WarnContext(ctx, "Failed to verify CSRF token", "path", r.URL.Path, "method", r.Method)
				}
			} else {
				slog.WarnContext(ctx, "CSRF token is missing", "path", r.URL.Path, "method", r.Method)
			}

			common.Redirect(s.RelURL(common.ExpiredEndpoint), http.StatusUnauthorized, w, r)
		})
	}
}
