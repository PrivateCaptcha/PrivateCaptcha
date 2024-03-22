package portal

import (
	"context"
	"log/slog"
	"net/http"
)

type errorRenderContext struct {
	ErrorCode    int
	ErrorMessage string
	Detail       string
}

func (s *Server) renderError(ctx context.Context, w http.ResponseWriter, code int) {
	slog.DebugContext(ctx, "Rendering error page", "code", code)

	data := &errorRenderContext{
		ErrorCode:    code,
		ErrorMessage: http.StatusText(code),
	}

	switch code {
	case http.StatusForbidden:
		data.Detail = "You don't have access to this page."
	case http.StatusNotFound:
		data.Detail = "This page does not exist."
	case http.StatusUnauthorized:
		data.Detail = "You need to log in to view this page."
	default:
		data.Detail = "Sorry, an unexpected error has occurred. Our team has been notified."
	}

	s.render(ctx, w, "errors/error.html", data)
}

func (s *Server) expired(w http.ResponseWriter, r *http.Request) {
	data := &errorRenderContext{
		ErrorCode:    http.StatusForbidden,
		ErrorMessage: "Session expired",
		Detail:       "Please begin again.",
	}

	s.render(r.Context(), w, "errors/error.html", data)
}
