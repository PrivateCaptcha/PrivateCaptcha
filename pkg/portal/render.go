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
	LoginEndpoint                  string
	TwoFactorEndpoint              string
	ResendEndpoint                 string
	RegisterEndpoint               string
	SettingsEndpoint               string
	LogoutEndpoint                 string
	NewEndpoint                    string
	OrgEndpoint                    string
	PropertyEndpoint               string
	DashboardEndpoint              string
	TabEndpoint                    string
	ReportsEndpoint                string
	IntegrationsEndpoint           string
	EditEndpoint                   string
	Token                          string
	Email                          string
	Name                           string
	Tab                            string
	VerificationCode               string
	Domain                         string
	Difficulty                     string
	Growth                         string
	Stats                          string
	DeleteEndpoint                 string
	MembersEndpoint                string
	OrgLevelInvited                string
	OrgLevelMember                 string
	OrgLevelOwner                  string
	GeneralEndpoint                string
	EmailEndpoint                  string
	UserEndpoint                   string
	APIKeysEndpoint                string
	Days                           string
	HeaderCSRFToken                string
	UsageEndpoint                  string
	NotificationEndpoint           string
	ErrorEndpoint                  string
	ValidityInterval               string
	AllowSubdomains                string
	AllowLocalhost                 string
	AllowReplay                    string
	IgnoreError                    string
	Terms                          string
	MaxReplayCount                 string
	MoveEndpoint                   string
	TransferEndpoint               string
	Org                            string
	User                           string
	AuditLogsEndpoint              string
	EventsEndpoint                 string
	Page                           string
	ExportEndpoint                 string
	Scope                          string
	APIKeyScopePuzzle              string
	APIKeyScopePortalReadWrite     string
	APIKeyScopePortalReadOnly      string
	PropertiesEndpoint             string
	All                            string
	ConditionUserAgent             string
	ConditionIPAddress             string
	ConditionCountryCode           string
	ConditionProperty              string
	ConditionOperator              string
	ConditionValue                 string
	ActionProperty                 string
	ActionValue                    string
	ConditionPropertyUserAgent     string
	ConditionPropertyIPAddress     string
	ConditionPropertyCountryCode   string
	OperatorEquals                 string
	OperatorContains               string
	OperatorEmpty                  string
	OperatorMatches                string
	OperatorIn                     string
	StringOperators                []string
	IPOperators                    []string
	GrowthTypeConstant             string
	GrowthTypeSlow                 string
	GrowthTypeMedium               string
	GrowthTypeFast                 string
	ActionPropertyDifficultyLevel  string
	ActionPropertyDifficultyGrowth string
	ActionPropertyHTTPRequest      string
	Enabled                        string
	ConditionNegated               string
	RulesEndpoint                  string
}

func NewRenderConstants() *RenderConstants {
	return &RenderConstants{
		LoginEndpoint:                  common.LoginEndpoint,
		TwoFactorEndpoint:              common.TwoFactorEndpoint,
		ResendEndpoint:                 common.ResendEndpoint,
		RegisterEndpoint:               common.RegisterEndpoint,
		SettingsEndpoint:               common.SettingsEndpoint,
		LogoutEndpoint:                 common.LogoutEndpoint,
		OrgEndpoint:                    common.OrgEndpoint,
		PropertyEndpoint:               common.PropertyEndpoint,
		DashboardEndpoint:              common.DashboardEndpoint,
		NewEndpoint:                    common.NewEndpoint,
		Token:                          common.ParamCSRFToken,
		Email:                          common.ParamEmail,
		Name:                           common.ParamName,
		Tab:                            common.ParamTab,
		VerificationCode:               common.ParamVerificationCode,
		Domain:                         common.ParamDomain,
		Difficulty:                     common.ParamDifficulty,
		Growth:                         common.ParamGrowth,
		Stats:                          common.StatsEndpoint,
		TabEndpoint:                    common.TabEndpoint,
		ReportsEndpoint:                common.ReportsEndpoint,
		IntegrationsEndpoint:           common.IntegrationsEndpoint,
		EditEndpoint:                   common.EditEndpoint,
		DeleteEndpoint:                 common.DeleteEndpoint,
		MembersEndpoint:                common.MembersEndpoint,
		OrgLevelInvited:                string(dbgen.AccessLevelInvited),
		OrgLevelMember:                 string(dbgen.AccessLevelMember),
		OrgLevelOwner:                  string(dbgen.AccessLevelOwner),
		GeneralEndpoint:                common.GeneralEndpoint,
		EmailEndpoint:                  common.EmailEndpoint,
		UserEndpoint:                   common.UserEndpoint,
		APIKeysEndpoint:                common.APIKeysEndpoint,
		Days:                           common.ParamDays,
		HeaderCSRFToken:                common.HeaderCSRFToken,
		UsageEndpoint:                  common.UsageEndpoint,
		NotificationEndpoint:           common.NotificationEndpoint,
		ErrorEndpoint:                  common.ErrorEndpoint,
		ValidityInterval:               common.ParamValidityInterval,
		AllowSubdomains:                common.ParamAllowSubdomains,
		AllowLocalhost:                 common.ParamAllowLocalhost,
		AllowReplay:                    common.ParamAllowReplay,
		IgnoreError:                    common.ParamIgnoreError,
		Terms:                          common.ParamTerms,
		MaxReplayCount:                 common.ParamMaxReplayCount,
		MoveEndpoint:                   common.MoveEndpoint,
		TransferEndpoint:               common.TransferEndpoint,
		Org:                            common.ParamOrg,
		User:                           common.ParamUser,
		AuditLogsEndpoint:              common.AuditLogsEndpoint,
		EventsEndpoint:                 common.EventsEndpoint,
		Page:                           common.ParamPage,
		ExportEndpoint:                 common.ExportEndpoint,
		Scope:                          common.ParamScope,
		APIKeyScopePuzzle:              apiKeyScopePuzzle,
		APIKeyScopePortalReadWrite:     apiKeyScopePortal + apiKeyReadWriteSuffix,
		APIKeyScopePortalReadOnly:      apiKeyScopePortal + apiKeyReadOnlySuffix,
		PropertiesEndpoint:             common.PropertiesEndpoint,
		All:                            common.All,
		ConditionUserAgent:             string(dbgen.RuleConditionPropertyUserAgent),
		ConditionIPAddress:             string(dbgen.RuleConditionPropertyIPAddress),
		ConditionCountryCode:           string(dbgen.RuleConditionPropertyCountryCode),
		OperatorEquals:                 string(dbgen.RuleConditionOperatorEquals),
		OperatorContains:               string(dbgen.RuleConditionOperatorContains),
		OperatorEmpty:                  string(dbgen.RuleConditionOperatorEmpty),
		OperatorMatches:                string(dbgen.RuleConditionOperatorMatches),
		OperatorIn:                     string(dbgen.RuleConditionOperatorIn),
		StringOperators:                []string{string(dbgen.RuleConditionOperatorEquals), string(dbgen.RuleConditionOperatorContains), string(dbgen.RuleConditionOperatorEmpty)},
		IPOperators:                    []string{string(dbgen.RuleConditionOperatorMatches), string(dbgen.RuleConditionOperatorEmpty)},
		ConditionProperty:              common.ParamConditionProperty,
		ConditionPropertyUserAgent:     string(dbgen.RuleConditionPropertyUserAgent),
		ConditionPropertyIPAddress:     string(dbgen.RuleConditionPropertyIPAddress),
		ConditionPropertyCountryCode:   string(dbgen.RuleConditionPropertyCountryCode),
		GrowthTypeConstant:             string(dbgen.DifficultyGrowthConstant),
		GrowthTypeSlow:                 string(dbgen.DifficultyGrowthSlow),
		GrowthTypeMedium:               string(dbgen.DifficultyGrowthMedium),
		GrowthTypeFast:                 string(dbgen.DifficultyGrowthFast),
		ActionPropertyDifficultyLevel:  string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		ActionPropertyDifficultyGrowth: string(dbgen.RuleActionPropertyDifficultyGrowth),
		ActionPropertyHTTPRequest:      string(dbgen.RuleActionPropertyHTTPRequest),
		ActionProperty:                 common.ParamActionProperty,
		ConditionOperator:              common.ParamConditionOperator,
		ConditionValue:                 common.ParamConditionValue,
		ActionValue:                    common.ParamActionValue,
		Enabled:                        common.ParamEnabled,
		ConditionNegated:               common.ParamConditionNegated,
		RulesEndpoint:                  common.RulesEndpoint,
	}
}

func (s *Server) RenderResponse(ctx context.Context, name string, data interface{}, reqCtx *RequestContext, platformCtx interface{}) (bytes.Buffer, error) {
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
