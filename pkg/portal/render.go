package portal

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

type RenderConstants struct {
	FeatureArgon2ID                 string
	FeatureEdgeTokens               string
	LoginEndpoint                   string
	TwoFactorEndpoint               string
	ResendEndpoint                  string
	RegisterEndpoint                string
	SettingsEndpoint                string
	LogoutEndpoint                  string
	NewEndpoint                     string
	OrgEndpoint                     string
	FormEndpoint                    string
	PropertyEndpoint                string
	FormsEndpoint                   string
	DashboardEndpoint               string
	TabEndpoint                     string
	ReportsEndpoint                 string
	IntegrationsEndpoint            string
	EditEndpoint                    string
	EdgeEndpoint                    string
	Token                           string
	Email                           string
	Name                            string
	ID                              string
	URL                             string
	Method                          string
	Tab                             string
	VerificationCode                string
	Domain                          string
	Difficulty                      string
	Growth                          string
	Challenge                       string
	ChallengeTypeBlake2b            string
	ChallengeTypeArgon2ID           string
	Stats                           string
	DeleteEndpoint                  string
	MembersEndpoint                 string
	OrgLevelInvited                 string
	OrgLevelMember                  string
	OrgLevelOwner                   string
	GeneralEndpoint                 string
	EmailEndpoint                   string
	UserEndpoint                    string
	APIKeysEndpoint                 string
	Days                            string
	Never                           string
	HeaderCSRFToken                 string
	UsageEndpoint                   string
	NotificationEndpoint            string
	ErrorEndpoint                   string
	ValidityInterval                string
	EdgeTokenValidityInterval       string
	EdgeWidgetStartMode             string
	EdgeWidgetStartModeClick        string
	EdgeWidgetStartModeLoad         string
	AllowSubdomains                 string
	AllowLocalhost                  string
	AllowReplay                     string
	IgnoreError                     string
	Terms                           string
	MaxReplayCount                  string
	MoveEndpoint                    string
	TransferEndpoint                string
	Org                             string
	User                            string
	AuditLogsEndpoint               string
	EventsEndpoint                  string
	Page                            string
	Offset                          string
	Sort                            string
	ExportEndpoint                  string
	Scope                           string
	APIKeyScopePuzzle               string
	APIKeyScopePortalReadWrite      string
	APIKeyScopePortalReadOnly       string
	PropertiesEndpoint              string
	All                             string
	OrgInviteEndpoint               string
	ConditionProperty               string
	ConditionOperator               string
	ConditionValue                  string
	ActionProperty                  string
	ActionValue                     string
	ConditionPropertyUserAgent      string
	ConditionPropertyIPAddress      string
	ConditionPropertyCountryCode    string
	ConditionPropertyDomain         string
	ConditionPropertyHTTPHeaderName string
	ConditionPropertyAlways         string
	ConditionPropertyBrowserVersion string
	OperatorEquals                  string
	OperatorContains                string
	OperatorEmpty                   string
	OperatorMatches                 string
	OperatorIn                      string
	OperatorBot                     string
	OperatorMore                    string
	StringOperators                 []string
	IPOperators                     []string
	GrowthTypeConstant              string
	GrowthTypeSlow                  string
	GrowthTypeMedium                string
	GrowthTypeFast                  string
	ActionPropertyDifficultyLevel   string
	ActionPropertyDifficultyGrowth  string
	ActionPropertyEdgeTokenValidity string
	ActionPropertyHTTPRequest       string
	ActionPropertyBreak             string
	Enabled                         string
	Active                          string
	RetryRequestCount               string
	RequestsPerMinute               string
	ConditionNegated                string
	RulesEndpoint                   string
	RuleStatsEndpoint               string
	Terminal                        string
	NotificationsEndpoint           string
	WeeklyReport                    string
	MonthlyReport                   string
	ClientSetupEndpoint             string
	ServerSetupEndpoint             string
	TestEndpoint                    string
	Body                            string
	Query                           string
	SearchEndpoint                  string
}

func NewRenderConstants() *RenderConstants {
	return &RenderConstants{
		FeatureArgon2ID:                 common.FeatureArgon2ID,
		FeatureEdgeTokens:               common.FeatureEdgeTokens,
		LoginEndpoint:                   common.LoginEndpoint,
		TwoFactorEndpoint:               common.TwoFactorEndpoint,
		ResendEndpoint:                  common.ResendEndpoint,
		RegisterEndpoint:                common.RegisterEndpoint,
		SettingsEndpoint:                common.SettingsEndpoint,
		LogoutEndpoint:                  common.LogoutEndpoint,
		OrgEndpoint:                     common.OrgEndpoint,
		FormEndpoint:                    common.FormEndpoint,
		PropertyEndpoint:                common.PropertyEndpoint,
		FormsEndpoint:                   common.FormsEndpoint,
		DashboardEndpoint:               common.DashboardEndpoint,
		NewEndpoint:                     common.NewEndpoint,
		Token:                           common.ParamCSRFToken,
		Email:                           common.ParamEmail,
		Name:                            common.ParamName,
		ID:                              common.ParamID,
		URL:                             common.ParamURL,
		Method:                          common.ParamMethod,
		Tab:                             common.ParamTab,
		VerificationCode:                common.ParamVerificationCode,
		Domain:                          common.ParamDomain,
		Difficulty:                      common.ParamDifficulty,
		Growth:                          common.ParamGrowth,
		Challenge:                       common.ParamChallenge,
		ChallengeTypeBlake2b:            string(dbgen.ChallengeTypeBlake2b),
		ChallengeTypeArgon2ID:           string(dbgen.ChallengeTypeArgon2ID),
		Stats:                           common.StatsEndpoint,
		TabEndpoint:                     common.TabEndpoint,
		ReportsEndpoint:                 common.ReportsEndpoint,
		IntegrationsEndpoint:            common.IntegrationsEndpoint,
		EditEndpoint:                    common.EditEndpoint,
		EdgeEndpoint:                    common.EdgeEndpoint,
		DeleteEndpoint:                  common.DeleteEndpoint,
		MembersEndpoint:                 common.MembersEndpoint,
		OrgLevelInvited:                 string(dbgen.AccessLevelInvited),
		OrgLevelMember:                  string(dbgen.AccessLevelMember),
		OrgLevelOwner:                   string(dbgen.AccessLevelOwner),
		GeneralEndpoint:                 common.GeneralEndpoint,
		EmailEndpoint:                   common.EmailEndpoint,
		UserEndpoint:                    common.UserEndpoint,
		APIKeysEndpoint:                 common.APIKeysEndpoint,
		Days:                            common.ParamDays,
		Never:                           common.ParamNever,
		HeaderCSRFToken:                 common.HeaderCSRFToken,
		UsageEndpoint:                   common.UsageEndpoint,
		NotificationEndpoint:            common.NotificationEndpoint,
		ErrorEndpoint:                   common.ErrorEndpoint,
		ValidityInterval:                common.ParamValidityInterval,
		EdgeTokenValidityInterval:       common.ParamEdgeTokenValidityInterval,
		EdgeWidgetStartMode:             common.ParamEdgeWidgetStartMode,
		EdgeWidgetStartModeClick:        string(dbgen.EdgeWidgetStartModeClick),
		EdgeWidgetStartModeLoad:         string(dbgen.EdgeWidgetStartModeLoad),
		AllowSubdomains:                 common.ParamAllowSubdomains,
		AllowLocalhost:                  common.ParamAllowLocalhost,
		AllowReplay:                     common.ParamAllowReplay,
		IgnoreError:                     common.ParamIgnoreError,
		Terms:                           common.ParamTerms,
		MaxReplayCount:                  common.ParamMaxReplayCount,
		MoveEndpoint:                    common.MoveEndpoint,
		TransferEndpoint:                common.TransferEndpoint,
		Org:                             common.ParamOrg,
		User:                            common.ParamUser,
		AuditLogsEndpoint:               common.AuditLogsEndpoint,
		EventsEndpoint:                  common.EventsEndpoint,
		Page:                            common.ParamPage,
		Offset:                          common.ParamOffset,
		Sort:                            common.ParamSort,
		ExportEndpoint:                  common.ExportEndpoint,
		Scope:                           common.ParamScope,
		APIKeyScopePuzzle:               apiKeyScopePuzzle,
		APIKeyScopePortalReadWrite:      apiKeyScopePortal + apiKeyReadWriteSuffix,
		APIKeyScopePortalReadOnly:       apiKeyScopePortal + apiKeyReadOnlySuffix,
		PropertiesEndpoint:              common.PropertiesEndpoint,
		All:                             common.All,
		OrgInviteEndpoint:               common.OrgInviteEndpoint,
		OperatorEquals:                  string(dbgen.RuleConditionOperatorEquals),
		OperatorContains:                string(dbgen.RuleConditionOperatorContains),
		OperatorEmpty:                   string(dbgen.RuleConditionOperatorEmpty),
		OperatorMatches:                 string(dbgen.RuleConditionOperatorMatches),
		OperatorIn:                      string(dbgen.RuleConditionOperatorIn),
		OperatorBot:                     string(dbgen.RuleConditionOperatorBot),
		OperatorMore:                    string(dbgen.RuleConditionOperatorMore),
		StringOperators:                 []string{string(dbgen.RuleConditionOperatorEquals), string(dbgen.RuleConditionOperatorContains), string(dbgen.RuleConditionOperatorEmpty)},
		IPOperators:                     []string{string(dbgen.RuleConditionOperatorMatches), string(dbgen.RuleConditionOperatorEmpty)},
		ConditionProperty:               common.ParamConditionProperty,
		ConditionPropertyUserAgent:      string(dbgen.RuleConditionPropertyUserAgent),
		ConditionPropertyIPAddress:      string(dbgen.RuleConditionPropertyIPAddress),
		ConditionPropertyCountryCode:    string(dbgen.RuleConditionPropertyCountryCode),
		ConditionPropertyDomain:         string(dbgen.RuleConditionPropertyDomain),
		ConditionPropertyHTTPHeaderName: string(dbgen.RuleConditionPropertyHTTPHeaderName),
		ConditionPropertyAlways:         string(dbgen.RuleConditionPropertyAlways),
		ConditionPropertyBrowserVersion: string(dbgen.RuleConditionPropertyBrowserVersion),
		GrowthTypeConstant:              string(dbgen.DifficultyGrowthConstant),
		GrowthTypeSlow:                  string(dbgen.DifficultyGrowthSlow),
		GrowthTypeMedium:                string(dbgen.DifficultyGrowthMedium),
		GrowthTypeFast:                  string(dbgen.DifficultyGrowthFast),
		ActionPropertyDifficultyLevel:   string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		ActionPropertyDifficultyGrowth:  string(dbgen.RuleActionPropertyDifficultyGrowth),
		ActionPropertyEdgeTokenValidity: string(dbgen.RuleActionPropertyEdgeTokenValidityInterval),
		ActionPropertyHTTPRequest:       string(dbgen.RuleActionPropertyHTTPRequest),
		ActionPropertyBreak:             string(dbgen.RuleActionPropertyBreak),
		ActionProperty:                  common.ParamActionProperty,
		ConditionOperator:               common.ParamConditionOperator,
		ConditionValue:                  common.ParamConditionValue,
		ActionValue:                     common.ParamActionValue,
		Enabled:                         common.ParamEnabled,
		Active:                          common.ParamActive,
		RetryRequestCount:               common.ParamRetryRequestCount,
		RequestsPerMinute:               common.ParamRequestsPerMinute,
		ConditionNegated:                common.ParamConditionNegated,
		RulesEndpoint:                   common.RulesEndpoint,
		RuleStatsEndpoint:               common.RuleStatsEndpoint,
		Terminal:                        common.ParamTerminal,
		NotificationsEndpoint:           common.NotificationsEndpoint,
		WeeklyReport:                    common.ParamWeeklyReport,
		MonthlyReport:                   common.ParamMonthlyReport,
		ClientSetupEndpoint:             common.ClientSetupEndpoint,
		ServerSetupEndpoint:             common.ServerSetupEndpoint,
		TestEndpoint:                    common.TestEndpoint,
		Body:                            common.ParamBody,
		Query:                           common.ParamQuery,
		SearchEndpoint:                  common.SearchEndpoint,
	}
}

type RenderFeatureFlags interface {
	Enabled(feature, orgID string) bool
}

type renderFeatureFlags struct {
	ctx      context.Context
	flags    common.FeatureFlags
	userID   *int32
	idHasher common.IdentifierHasher
}

func (f *renderFeatureFlags) Enabled(feature, orgRef string) bool {
	var orgID *int32
	if orgRef != "" {
		id, err := f.idHasher.Decrypt(orgRef)
		if err != nil || id <= 0 || id > math.MaxInt32 {
			slog.WarnContext(f.ctx, "Invalid organization ID for feature check", "org", orgRef, common.ErrAttr(err))
			return false
		}
		orgID = new(int32)
		*orgID = int32(id)
	}
	return f.flags.Enabled(f.ctx, feature, f.userID, orgID)
}

func (s *Server) renderFeatures(ctx context.Context, userID *int32) RenderFeatureFlags {
	return &renderFeatureFlags{
		ctx:      ctx,
		flags:    s.FeatureFlags,
		userID:   userID,
		idHasher: s.IDHasher,
	}
}

func (s *Server) RenderResponse(ctx context.Context, name string, data interface{}, reqCtx *RequestContext, platformCtx interface{}) (*bytes.Buffer, error) {
	if reqCtx.Features == nil {
		reqCtx.Features = s.renderFeatures(ctx, nil)
	}

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

	out := &bytes.Buffer{}

	if err := ctx.Err(); err != nil {
		return out, err
	}

	err := s.template.Render(ctx, out, name, actualData)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to render template", "name", name, common.ErrAttr(err))
	}

	return out, err
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data interface{}, isNew bool) {
	ctx := r.Context()

	reqCtx := &RequestContext{
		Path:        r.URL.Path,
		CurrentYear: time.Now().Year(),
		CDN:         s.CDNURL,
		API:         s.APIURL,
	}

	if loggedIn, ok := ctx.Value(common.LoggedInContextKey).(bool); ok && loggedIn {
		reqCtx.LoggedIn = true
	}

	pathPattern, ok := ctx.Value(common.PathPatternContextKey).(string)
	if ok && len(pathPattern) > 0 {
		reqCtx.Pattern = common.RelURL(s.Prefix, pathPattern)
	}

	var userID *int32
	if sess, err := s.Sessions.Get(r); err == nil {
		if authority, ok := sess.Authority(); ok && authority.State == session.StateAuthenticated && authority.UserID > 0 {
			userID = &authority.UserID
		}
		if username, ok := sess.Get(ctx, session.KeyUserName).(string); ok {
			reqCtx.UserName = username
		}

		if reqCtx.LoggedIn {
			if _, ok := sess.Get(ctx, session.KeyFirstSession).(bool); ok {
				reqCtx.FirstSession = true
			}

			if tipAllowed, ok := ctx.Value(common.TipContextKey).(bool); ok && tipAllowed {
				if tipIndex, ok := sess.Get(ctx, session.KeyTip).(int); ok && (tipIndex >= 0) && (tipIndex < len(s.Tips)) {
					tip := s.Tips[tipIndex]
					matchPath := common.RelURL("", pathPattern)
					if slices.Contains(tip.Patterns, matchPath) {
						reqCtx.Tip = tip
						// we (attempt to) show tip only once
						_ = sess.Delete(ctx, session.KeyTip)
					}
				}
			}
		}
	}

	reqCtx.Features = s.renderFeatures(ctx, userID)
	out, err := s.RenderResponse(ctx, name, data, reqCtx, s.PlatformCtx)
	if err == nil {
		common.WriteHeaders(w, common.SecurityHeaders)
		common.WriteHeaders(w, common.HtmlContentHeaders)

		if isNew && (len(reqCtx.Pattern) > 0) {
			if _, ok := r.Header[common.HeaderHtmxRequest]; ok {
				w.Header().Set(common.HeaderCaptchaPushURL, reqCtx.Pattern)
			}
		}

		w.WriteHeader(http.StatusOK)
		if _, werr := out.WriteTo(w); werr != nil {
			slog.ErrorContext(ctx, "Failed to write response", common.ErrAttr(werr))
		}
	} else {
		errorStatus := http.StatusInternalServerError
		switch err {
		case context.DeadlineExceeded:
			errorStatus = http.StatusGatewayTimeout
		case context.Canceled:
			return
		}
		s.renderError(ctx, w, r, errorStatus)
	}
}
