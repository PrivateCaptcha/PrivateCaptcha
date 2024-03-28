package portal

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
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

	reqCtx := struct {
		LoggedIn bool
	}{
		LoggedIn: false,
	}

	actualData := struct {
		Params interface{}
		Const  interface{}
		Ctx    interface{}
	}{
		Params: data,
		Const:  renderConstants,
		Ctx:    reqCtx,
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

	if err := s.template.Render(ctx, w, "errors/error.html", actualData); err != nil {
		slog.ErrorContext(ctx, "Failed to render error template", common.ErrAttr(err))
	}
}

func (s *Server) expired(w http.ResponseWriter, r *http.Request) {
	data := &errorRenderContext{
		ErrorCode:    http.StatusForbidden,
		ErrorMessage: "Session expired",
		Detail:       "Please begin again.",
	}

	s.render(r.Context(), w, r, "errors/error.html", data)
}
