package portal

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/PrivateCaptcha/PrivateCaptcha/web"
)

const (
	maxLoginFormSizeBytes = 10 * 1024
)

func funcMap(prefix string) template.FuncMap {
	return template.FuncMap{
		"qescape": url.QueryEscape,
		"safeHTML": func(s string) any {
			return template.HTML(s)
		},
		"relURL": func(s string) any {
			s = strings.TrimPrefix(s, "/")
			p := strings.TrimSuffix(prefix, "/")
			return p + "/" + s
		},
	}
}

type Server struct {
	Store    *db.Store
	Prefix   string
	template *web.Template
	XSRF     XSRFMiddleware
	Session  session.Manager
}

func (s *Server) Setup(router *http.ServeMux) {
	prefix := "/" + strings.Trim(s.Prefix, "/") + "/"
	s.setupWithPrefix(prefix, router)
}

func (s *Server) setupWithPrefix(prefix string, router *http.ServeMux) {
	slog.Debug("Setting up the routes", "prefix", prefix)

	s.Session.Path = prefix
	s.template = web.NewTemplates(funcMap(prefix))

	router.HandleFunc(prefix+common.LoginEndpoint, common.Logged(s.login))
	//router.HandleFunc(prefix+"/", common.Logged(s.Session.Auth()))
}

func (s *Server) render(ctx context.Context, w http.ResponseWriter, name string, data interface{}) {
	if err := s.template.Render(ctx, w, name, data); err != nil {
		slog.ErrorContext(ctx, "Failed to render template", common.ErrAttr(err))
		// TODO: Redirect to internal error status page instead
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		data := struct {
			Token  string
			Prefix string
		}{
			Token:  s.XSRF.Token("", actionLogin),
			Prefix: s.Prefix,
		}

		s.render(ctx, w, "login.html", data)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginFormSizeBytes)
		err := r.ParseForm()
		if err != nil {
			slog.ErrorContext(r.Context(), "Failed to read request body", common.ErrAttr(err))
			// TODO: Redirect to error page instead
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		email := r.FormValue(common.ParamEmail)
		_, err = s.Store.FindUser(ctx, email)
		if err != nil {
			data := struct {
				Error string
			}{
				Error: "User with such email does not exist",
			}

			s.render(ctx, w, "login-email-error.html", data)
			return
		}
		// TODO: Do the actual authentication
		// redirect to email 2fa
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
