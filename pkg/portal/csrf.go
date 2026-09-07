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

func (s *Server) csrfPendingEmailAuthorityKeyFunc(_ http.ResponseWriter, r *http.Request) string {
	sess, err := s.Sessions.Get(r)
	if err != nil {
		return ""
	}
	authority, ok := sess.Authority()
	if !ok || authority.State != session.StatePending || authority.ChallengeEmail == "" {
		slog.WarnContext(r.Context(), "Session does not contain pending challenge Authority")
		return ""
	}
	return authority.ChallengeEmail
}

func (s *Server) csrfAuthenticatedUserAuthorityKeyFunc(_ http.ResponseWriter, r *http.Request) string {
	sess, err := s.Sessions.Get(r)
	if err != nil {
		return ""
	}
	authority, ok := sess.Authority()
	if !ok || authority.State != session.StateAuthenticated || authority.UserID <= 0 {
		slog.WarnContext(r.Context(), "Session does not contain authenticated user Authority")
		return ""
	}
	return strconv.Itoa(int(authority.UserID))
}

func (s *Server) verifyCSRF(w http.ResponseWriter, r *http.Request, key string) bool {
	ctx := r.Context()
	token := r.Header.Get(common.HeaderCSRFToken)
	if len(token) == 0 {
		token = r.FormValue(common.ParamCSRFToken)
	}
	if len(token) > 0 && s.XSRF.VerifyToken(token, key) {
		return true
	}
	if len(token) == 0 {
		slog.WarnContext(ctx, "CSRF token is missing", "path", r.URL.Path, "method", r.Method)
	} else {
		slog.WarnContext(ctx, "Failed to verify CSRF token", "path", r.URL.Path, "method", r.Method, "key", key)
	}
	common.Redirect(s.RelURL(common.ExpiredEndpoint), http.StatusUnauthorized, w, r)
	return false
}

// csrfPendingChallenge verifies sign-in requests; registration verifies in postTwoFactor before consumption.
func (s *Server) csrfPendingChallenge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.Sessions.Get(r)
		if err != nil {
			common.Redirect(s.RelURL(common.ExpiredEndpoint), http.StatusUnauthorized, w, r)
			return
		}
		authority, ok := sess.Authority()
		if !ok || authority.State != session.StatePending {
			common.Redirect(s.RelURL(common.ExpiredEndpoint), http.StatusUnauthorized, w, r)
			return
		}
		if authority.ChallengeKind == session.ChallengeKindRegistration ||
			(authority.ChallengeKind == session.ChallengeKindSignIn && s.verifyCSRF(w, r, authority.ChallengeEmail)) {
			next.ServeHTTP(w, r)
			return
		}
		if authority.ChallengeKind != session.ChallengeKindSignIn {
			common.Redirect(s.RelURL(common.ExpiredEndpoint), http.StatusUnauthorized, w, r)
		}
	})
}

func (s *Server) csrf(keyFunc CsrfKeyFunc) alice.Constructor {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
				next.ServeHTTP(w, r)
				return
			}

			if s.verifyCSRF(w, r, keyFunc(w, r)) {
				next.ServeHTTP(w, r)
			}
		})
	}
}
