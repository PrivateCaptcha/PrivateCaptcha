package portal

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

// RenderContext is the interface that all portal view models must implement.
// Params returns the view-specific data and Const returns the view-specific constants.
type RenderContext interface {
	Params() interface{}
	Const() interface{}
}

type BaseRenderConstants struct {
	HeaderCSRFToken      string
	SettingsEndpoint     string
	TabEndpoint          string
	AuditLogsEndpoint    string
	LogoutEndpoint       string
	ErrorEndpoint        string
	NotificationEndpoint string
}

var baseConst = BaseRenderConstants{
	HeaderCSRFToken:      common.HeaderCSRFToken,
	SettingsEndpoint:     common.SettingsEndpoint,
	TabEndpoint:          common.TabEndpoint,
	AuditLogsEndpoint:    common.AuditLogsEndpoint,
	LogoutEndpoint:       common.LogoutEndpoint,
	ErrorEndpoint:        common.ErrorEndpoint,
	NotificationEndpoint: common.NotificationEndpoint,
}

func (s *Server) RenderResponse(ctx context.Context, name string, data RenderContext, reqCtx *RequestContext, platformCtx interface{}) (bytes.Buffer, error) {
	actualData := struct {
		Params   interface{}
		Const    interface{}
		Ctx      interface{}
		Platform interface{}
		Data     interface{}
	}{
		Params:   data.Params(),
		Const:    data.Const(),
		Ctx:      reqCtx,
		Platform: platformCtx,
		Data:     s.DataCtx,
	}

	var out bytes.Buffer

	if err := ctx.Err(); err == context.DeadlineExceeded {
		return out, err
	}

	err := s.template.Render(ctx, &out, name, actualData)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to render template", "name", name, common.ErrAttr(err))
	}

	return out, err
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data RenderContext) {
	ctx := r.Context()

	loggedIn, ok := ctx.Value(common.LoggedInContextKey).(bool)

	reqCtx := &RequestContext{
		Path:        r.URL.Path,
		LoggedIn:    ok && loggedIn,
		CurrentYear: time.Now().Year(),
		CDN:         s.CDNURL,
	}

	if sess, found := s.Sessions.SessionGet(r); found {
		if username, ok := sess.Get(ctx, session.KeyUserName).(string); ok {
			reqCtx.UserName = username
		}
	}

	out, err := s.RenderResponse(ctx, name, data, reqCtx, s.PlatformCtx)
	if err == nil {
		common.WriteHeaders(w, common.SecurityHeaders)
		common.WriteHeaders(w, common.HtmlContentHeaders)
		w.WriteHeader(http.StatusOK)
		if _, werr := out.WriteTo(w); werr != nil {
			slog.ErrorContext(ctx, "Failed to write response", common.ErrAttr(werr))
		}
	} else {
		errorStatus := http.StatusInternalServerError
		if err == context.DeadlineExceeded {
			errorStatus = http.StatusGatewayTimeout
		}
		s.renderError(ctx, w, errorStatus)
	}
}
