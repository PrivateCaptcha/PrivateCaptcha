package portal

import (
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

func funcMap(prefix string) template.FuncMap {
	return template.FuncMap{
		"qescape": url.QueryEscape,
		"safeHTML": func(s string) any {
			return template.HTML(s)
		},
		"relURL": func(s string) any {
			if strings.HasPrefix(s, "/") || strings.HasSuffix(prefix, "/") {
				return prefix + s
			} else {
				return prefix + "/" + s
			}
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
	prefix := s.Prefix

	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + s.Prefix
	}

	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	s.setupWithPrefix(prefix, router)
}

func (s *Server) setupWithPrefix(prefix string, router *http.ServeMux) {
	slog.Debug("Setting up the routes", "prefix", prefix)

	s.Session.Path = prefix
	s.template = web.NewTemplates(funcMap(prefix))

	router.HandleFunc(prefix+common.LoginEndpoint, common.Logged(s.login))
	//router.HandleFunc(prefix+"/", common.Logged(s.Session.Auth()))
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

		if err := s.template.Render(ctx, w, "login.html", data); err != nil {
			slog.ErrorContext(ctx, "Failed to render template", common.ErrAttr(err))
			// TODO: Redirect to internal error status page instead
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	case http.MethodPost:
		// TODO: Do the actual authentication
		// redirect to email 2fa
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
