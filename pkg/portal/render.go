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

type RenderConstants struct {
	LoginEndpoint        string
	TwoFactorEndpoint    string
	ResendEndpoint       string
	RegisterEndpoint     string
	SettingsEndpoint     string
	LogoutEndpoint       string
	NewEndpoint          string
	OrgEndpoint          string
	PropertyEndpoint     string
	DashboardEndpoint    string
	TabEndpoint          string
	ReportsEndpoint      string
	IntegrationsEndpoint string
	EditEndpoint         string
	Token                string
	Email                string
	Name                 string
	Tab                  string
	VerificationCode     string
	Domain               string
	Difficulty           string
	Growth               string
	Stats                string
	DeleteEndpoint       string
	MembersEndpoint      string
	OrgLevelInvited      string
	OrgLevelMember       string
	OrgLevelOwner        string
	GeneralEndpoint      string
	EmailEndpoint        string
	UserEndpoint         string
	APIKeysEndpoint      string
	Days                 string
	HeaderCSRFToken      string
	UsageEndpoint        string
	NotificationEndpoint string
	ErrorEndpoint        string
	ValidityInterval     string
	AllowSubdomains      string
	AllowLocalhost       string
	AllowReplay          string
	IgnoreError          string
	Terms                string
	MaxReplayCount       string
	MoveEndpoint         string
	Org                  string
	AuditLogsEndpoint    string
	EventsEndpoint       string
	Page                 string
	ExportEndpoint       string
	Scope                string
	APIKeyScopePuzzle    string
	APIKeyScopePortal    string
	PropertiesEndpoint   string
}

func NewRenderConstants() *RenderConstants {
	return &RenderConstants{
		LoginEndpoint:        common.LoginEndpoint,
		TwoFactorEndpoint:    common.TwoFactorEndpoint,
		ResendEndpoint:       common.ResendEndpoint,
		RegisterEndpoint:     common.RegisterEndpoint,
		SettingsEndpoint:     common.SettingsEndpoint,
		LogoutEndpoint:       common.LogoutEndpoint,
		OrgEndpoint:          common.OrgEndpoint,
		PropertyEndpoint:     common.PropertyEndpoint,
		DashboardEndpoint:    common.DashboardEndpoint,
		NewEndpoint:          common.NewEndpoint,
		Token:                common.ParamCSRFToken,
		Email:                common.ParamEmail,
		Name:                 common.ParamName,
		Tab:                  common.ParamTab,
		VerificationCode:     common.ParamVerificationCode,
		Domain:               common.ParamDomain,
		Difficulty:           common.ParamDifficulty,
		Growth:               common.ParamGrowth,
		Stats:                common.StatsEndpoint,
		TabEndpoint:          common.TabEndpoint,
		ReportsEndpoint:      common.ReportsEndpoint,
		IntegrationsEndpoint: common.IntegrationsEndpoint,
		EditEndpoint:         common.EditEndpoint,
		DeleteEndpoint:       common.DeleteEndpoint,
		MembersEndpoint:      common.MembersEndpoint,
		OrgLevelInvited:      string(dbgen.AccessLevelInvited),
		OrgLevelMember:       string(dbgen.AccessLevelMember),
		OrgLevelOwner:        string(dbgen.AccessLevelOwner),
		GeneralEndpoint:      common.GeneralEndpoint,
		EmailEndpoint:        common.EmailEndpoint,
		UserEndpoint:         common.UserEndpoint,
		APIKeysEndpoint:      common.APIKeysEndpoint,
		Days:                 common.ParamDays,
		HeaderCSRFToken:      common.HeaderCSRFToken,
		UsageEndpoint:        common.UsageEndpoint,
		NotificationEndpoint: common.NotificationEndpoint,
		ErrorEndpoint:        common.ErrorEndpoint,
		ValidityInterval:     common.ParamValidityInterval,
		AllowSubdomains:      common.ParamAllowSubdomains,
		AllowLocalhost:       common.ParamAllowLocalhost,
		AllowReplay:          common.ParamAllowReplay,
		IgnoreError:          common.ParamIgnoreError,
		Terms:                common.ParamTerms,
		MaxReplayCount:       common.ParamMaxReplayCount,
		MoveEndpoint:         common.MoveEndpoint,
		Org:                  common.ParamOrg,
		AuditLogsEndpoint:    common.AuditLogsEndpoint,
		EventsEndpoint:       common.EventsEndpoint,
		Page:                 common.ParamPage,
		ExportEndpoint:       common.ExportEndpoint,
		Scope:                common.ParamScope,
		APIKeyScopePuzzle:    string(dbgen.ApiKeyScopePuzzle),
		APIKeyScopePortal:    string(dbgen.ApiKeyScopePortal),
		PropertiesEndpoint:   common.PropertiesEndpoint,
	}
}

func (s *Server) RenderResponse(ctx context.Context, name string, data interface{}, reqCtx *RequestContext) (bytes.Buffer, error) {
	actualData := struct {
		Params   interface{}
		Const    interface{}
		Ctx      interface{}
		Platform interface{}
		Data     interface{}
	}{
		Params:   data,
		Const:    s.RenderConstants,
		Ctx:      reqCtx,
		Platform: s.PlatformCtx,
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

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
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

	out, err := s.RenderResponse(ctx, name, data, reqCtx)
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
