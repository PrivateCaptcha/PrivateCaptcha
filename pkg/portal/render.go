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
	Months               string
	HeaderCSRFToken      string
	UsageEndpoint        string
	NotificationEndpoint string
	LegalEndpoint        string
	PrivacyEndpoint      string
	ErrorEndpoint        string
	AboutEndpoint        string
	ValidityInterval     string
	AllowSubdomains      string
	AllowLocalhost       string
	AllowReplay          string
	IgnoreError          string
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
		Months:               common.ParamMonths,
		HeaderCSRFToken:      common.HeaderCSRFToken,
		UsageEndpoint:        common.UsageEndpoint,
		NotificationEndpoint: common.NotificationEndpoint,
		LegalEndpoint:        common.LegalEndpoint,
		PrivacyEndpoint:      common.PrivacyEndpoint,
		ErrorEndpoint:        common.ErrorEndpoint,
		AboutEndpoint:        common.AboutEndpoint,
		ValidityInterval:     common.ParamValidityInterval,
		AllowSubdomains:      common.ParamAllowSubdomains,
		AllowLocalhost:       common.ParamAllowLocalhost,
		AllowReplay:          common.ParamAllowReplay,
		IgnoreError:          common.ParamIgnoreError,
	}
}

func (s *Server) renderResponse(ctx context.Context, name string, data interface{}, reqCtx *RequestContext) (bytes.Buffer, error) {
	actualData := struct {
		Params interface{}
		Const  interface{}
		Ctx    interface{}
	}{
		Params: data,
		Const:  s.RenderConstants,
		Ctx:    reqCtx,
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

	sess := s.Sessions.SessionStart(w, r)
	if username, ok := sess.Get(session.KeyUserName).(string); ok {
		reqCtx.UserName = username
	}

	out, err := s.renderResponse(ctx, name, data, reqCtx)
	if err == nil {
		w.Header().Set(common.HeaderContentType, common.ContentTypeHTML)
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
