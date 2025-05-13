package portal

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	errorTemplate    = "errors/error.html"
	maxErrorBodySize = 512 * 1024
)

type errorRenderContext struct {
	CsrfRenderContext
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
		CDN      string
	}{
		LoggedIn: false,
		CDN:      s.CDNURL,
	}

	actualData := struct {
		Params interface{}
		Const  interface{}
		Ctx    interface{}
	}{
		Params: data,
		Const:  s.RenderConstants,
		Ctx:    reqCtx,
	}

	switch code {
	case http.StatusForbidden:
		data.Detail = "You don't have access to this page."
	case http.StatusNotFound:
		data.Detail = "This page does not exist."
	case http.StatusUnauthorized:
		data.Detail = "You need to log in to view this page."
	case http.StatusServiceUnavailable:
		data.Detail = "This page is temporarily unavailable. Please check back later."
	default:
		data.Detail = "Sorry, an unexpected error has occurred. Our team has been notified."
	}

	var out bytes.Buffer
	err := s.template.Render(ctx, &out, errorTemplate, actualData)
	if err == nil {
		w.Header().Set(common.HeaderContentType, common.ContentTypeHTML)
		common.WriteHeaders(w, common.CachedHeaders)
		w.WriteHeader(code)
		if _, werr := out.WriteTo(w); werr != nil {
			slog.ErrorContext(ctx, "Failed to write error page", common.ErrAttr(werr))
		}
	} else {
		slog.ErrorContext(ctx, "Failed to render error template", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (s *Server) error(w http.ResponseWriter, r *http.Request) {
	code, _ := strconv.Atoi(r.PathValue(common.ParamCode))
	if (code < 100) || (code > 600) {
		slog.ErrorContext(r.Context(), "Invalid error code", "code", code)
		code = http.StatusInternalServerError
	}

	s.renderError(r.Context(), w, code)
}

func (s *Server) RedirectError(code int, w http.ResponseWriter, r *http.Request) {
	url := s.relURL(common.ErrorEndpoint + "/" + strconv.Itoa(code))
	common.Redirect(url, code, w, r)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(r.Context(), w, http.StatusNotFound)
}

func (s *Server) expired(w http.ResponseWriter, r *http.Request) {
	data := &errorRenderContext{
		ErrorCode:    http.StatusForbidden,
		ErrorMessage: "Session expired",
		Detail:       "Please begin again.",
	}

	common.WriteHeaders(w, common.CachedHeaders)

	s.render(w, r, errorTemplate, data)
}

func (s *Server) postClientSideError(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxErrorBodySize)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read request body", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// the point of logging here is that we will have a connection to user's session
	slog.ErrorContext(ctx, "Client-side error occurred", "error", string(body))

	w.WriteHeader(http.StatusOK)
}
