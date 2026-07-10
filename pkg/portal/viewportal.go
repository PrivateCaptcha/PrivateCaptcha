//go:build viewportal

package portal

import (
	"fmt"
	"time"

	randv2 "math/rand/v2"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func arg(s string) string {
	return fmt.Sprintf("{%s}", s)
}

func viewStubOrg(id string) *UserOrg {
	return &UserOrg{Name: "Acme Corp", ID: id, Level: string(dbgen.AccessLevelOwner)}
}

func viewStubProperty(name, orgID string) *userProperty {
	return &userProperty{
		ID: "prop1", OrgID: orgID, Name: name, Domain: "example.com",
		Sitekey: db.TestPropertySitekey, Level: int(common.DifficultyLevelMedium),
		Growth: 2, ValidityInterval: 60, MaxReplayCount: 3,
		AllowSubdomains: true, AllowReplay: true, Enabled: true,
	}
}

func viewStubForm(name, orgID string) *userForm {
	return &userForm{
		ID:                "form1",
		OrgID:             orgID,
		PropertyID:        "prop1",
		Name:              name,
		URL:               "https://hooks.example.com/forms/contact",
		WebhookPrefix:     "https://example.com/api/...",
		ExternalID:        "123e4567e89b12d3a456426614174000",
		Enabled:           true,
		Active:            randv2.Int()%2 == 0,
		RetryRequestCount: 2,
		RequestsPerMinute: 30,
	}
}

func viewStubAuditLogs() []*UserAuditLog {
	actions := []dbgen.AuditLogAction{
		dbgen.AuditLogActionCreate, dbgen.AuditLogActionUpdate,
		dbgen.AuditLogActionDelete, dbgen.AuditLogActionAccess, dbgen.AuditLogActionLogin,
	}
	tables := []string{"properties", "organizations", "api_keys", "users", "org_users", "forms"}
	sources := []dbgen.AuditLogSource{dbgen.AuditLogSourcePortal, dbgen.AuditLogSourceApi}

	result := make([]*UserAuditLog, 0, len(actions))
	for i, table := range tables {
		result = append(result, &UserAuditLog{
			UserName:  "Jane Doe",
			UserEmail: "jane@example.com",
			Action:    string(actions[randv2.IntN(len(actions))]),
			Source:    string(sources[randv2.IntN(len(sources))]),
			Property:  "Main Site", Resource: "example.com",
			TableName: table,
			Time:      time.Now().Add(-time.Duration(i*30) * time.Minute).Format(auditLogTimeFormat),
		})
	}

	return result
}

func viewStubRules() []*DifficultyRuleModel {
	return []*DifficultyRuleModel{
		{
			ID: "r1", Name: "Block suspicious countries",
			ConditionProperty: string(dbgen.RuleConditionPropertyCountryCode),
			ConditionOperator: string(dbgen.RuleConditionOperatorIn),
			ConditionValue:    "AB, CD, EF", ActionProperty: string(dbgen.RuleActionPropertyHTTPRequest),
			ActionValue: "0", Enabled: true, CanEdit: true, Terminal: true,
		},
		{
			ID: "r2", Name: "Lower difficulty for mobile",
			ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
			ConditionOperator: string(dbgen.RuleConditionOperatorContains),
			ConditionValue:    "Mobile", ActionProperty: string(dbgen.RuleActionPropertyDifficultyLevelPercent),
			ActionValue: "-20", Enabled: true, CanEdit: true,
		},
		{
			ID: "r3", Name: "Block bots",
			ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
			ConditionOperator: string(dbgen.RuleConditionOperatorBot),
			ActionProperty:    string(dbgen.RuleActionPropertyHTTPRequest),
			ActionValue:       "0", Enabled: false, CanEdit: true,
		},
	}
}

func viewStubAPIKey(name string) *userAPIKey {
	return &userAPIKey{
		ID: "k1", Name: name,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Format("02 Jan 2006"),
		Scope:     "puzzle", RequestsPerMinute: 60, OrgName: "Acme Corp",
		LastUsedAt: time.Now().Add(-2 * time.Hour).Format("02 Jan 2006 15:04"),
	}
}

// BuildViewPortalPages returns all portal page configurations for the viewportal tool.
// Paths are constructed using the same patterns as setupWithPrefix.
func (s *Server) BuildViewPortalPages() []ViewPortalPage {
	org := viewStubOrg("org1")
	orgs := []*UserOrg{org, {Name: "Other Org", ID: "org2", Level: string(dbgen.AccessLevelOwner)}}
	prop := viewStubProperty("Main Site", "org1")
	form := viewStubForm("Contact us", "org1")
	token := CsrfRenderContext{Token: "stub-csrf-token"}
	rules := viewStubRules()
	auditLogs := viewStubAuditLogs()

	baseCtx := func(tab int) portalBaseRenderContext {
		return portalBaseRenderContext{
			CsrfRenderContext: token, Orgs: orgs, CurrentOrg: org, SortOptions: orgPropertiesSortOptions, Tab: tab, CanEdit: true,
		}
	}

	captchaCtx := CaptchaRenderContext{
		CaptchaEndpoint: "/" + common.PuzzleEndpoint, CaptchaSolutionField: "portal-solution",
		CaptchaSitekey: db.TestPropertySitekey, CaptchaDebug: true,
	}

	propDash := func(tab int, alert AlertRenderContext) propertyDashboardRenderContext {
		propCaptchaCtx := CaptchaRenderContext{
			CaptchaEndpoint: s.RelURL(common.EchoPuzzleEndpoint), CaptchaSolutionField: "portal-solution",
			CaptchaSitekey: db.TestPropertySitekey, CaptchaDebug: true,
		}
		return propertyDashboardRenderContext{
			AlertRenderContext: alert, CsrfRenderContext: token,
			CaptchaRenderContext: propCaptchaCtx, Property: prop, Org: org,
			Tab: tab, CanEdit: true, IncludeRules: true,
		}
	}

	formDash := func(tab int, alert AlertRenderContext) formDashboardRenderContext {
		return formDashboardRenderContext{
			AlertRenderContext: alert,
			CsrfRenderContext:  token,
			Form:               form,
			Org:                org,
			Tab:                tab,
			CanEdit:            true,
		}
	}

	settingsCommon := func(activeTab string, alert AlertRenderContext) SettingsCommonRenderContext {
		return SettingsCommonRenderContext{
			AlertRenderContext: alert, CsrfRenderContext: token,
			Tabs: CreateTabViewModels(activeTab, s.SettingsTabs), ActiveTabID: activeTab,
			Email: "jane@example.com", UserID: 1,
		}
	}

	auditLogsCtx := AuditLogsRenderContext{
		AuditLogs: auditLogs, Count: len(auditLogs), Page: 0, PerPage: perPageEventLogs,
	}

	pagination := PaginationRenderContext{From: 1, To: 2, Count: 2, Page: 0, PerPage: 30}

	members := []*orgUser{
		{Name: "Jane Doe", Email: "jane@example.com", ID: "u1", Level: string(dbgen.AccessLevelOwner), CreatedAt: "01 Jan 2025"},
		{Name: "John Smith", Email: "john@example.com", ID: "u2", Level: string(dbgen.AccessLevelMember), CreatedAt: "15 Mar 2025"},
		{Name: "Alice Johnson", Email: "alice@example.com", ID: "u3", Level: string(dbgen.AccessLevelMember), CreatedAt: "20 Mar 2025"},
		{Name: "Bob Williams", Email: "bob@example.com", ID: "u4", Level: string(dbgen.AccessLevelInvited), CreatedAt: "01 Apr 2025"},
		{Name: "", Email: "invite@example.com", ID: "u5", Level: string(dbgen.AccessLevelInvited), CreatedAt: "05 Apr 2025", IsEmailInvite: true},
		{Name: "", Email: "pending@company.com", ID: "u6", Level: string(dbgen.AccessLevelInvited), CreatedAt: "06 Apr 2025", IsEmailInvite: true},
	}

	// accepted non-owner members for org settings transfer dropdown
	transferMembers := []*orgUser{members[1], members[2]}

	countries := []CountryOption{
		{Code: "US", Name: "United States"}, {Code: "DE", Name: "Germany"}, {Code: "JP", Name: "Japan"},
	}

	// path helper using server prefix, matching setupWithPrefix patterns
	p := func(parts ...string) string {
		return s.PartsURL(parts...)
	}

	orgArg := arg(common.ParamOrg)
	propArg := arg(common.ParamProperty)
	formArg := arg(common.ParamForm)
	ruleArg := arg(common.ParamRule)

	return []ViewPortalPage{
		// --- Public pages ---
		{
			Path:       p(common.LoginEndpoint),
			Template:   loginTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &loginRenderContext{
					CsrfRenderContext: token, CaptchaRenderContext: captchaCtx,
					Email: "jane@example.com", CanRegister: true,
				}
			},
		},
		{
			Path:       p(common.RegisterEndpoint),
			Template:   loginTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &loginRenderContext{
					CsrfRenderContext: token, CaptchaRenderContext: captchaCtx,
					CanRegister: true, IsRegister: true,
				}
			},
		},
		{
			Path:       p(common.ErrorEndpoint, arg(common.ParamCode)),
			Template:   errorTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &errorRenderContext{
					CsrfRenderContext: token, ErrorCode: 404,
					ErrorMessage: "Not Found", Detail: "This page does not exist.",
				}
			},
		},
		{
			Path:       p(common.ExpiredEndpoint),
			Template:   errorTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &errorRenderContext{
					CsrfRenderContext: token, ErrorCode: 403,
					ErrorMessage: "Session expired", Detail: "Please begin again.",
				}
			},
		},
		{
			Path:       p(common.AccountVerifyEndpoint),
			Template:   accountVerifyTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &loginRenderContext{}
			},
		},
		// --- Settings pages ---
		{
			Path:       p(common.SettingsEndpoint),
			Template:   settingsGeneralTemplatePrefix + "page.html",
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsGeneralRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.GeneralEndpoint, a), Name: "Jane Doe",
				}
			},
		},
		{
			Path:     p(common.SettingsEndpoint, common.TabEndpoint, common.GeneralEndpoint),
			Template: settingsGeneralTemplatePrefix + "tab.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsGeneralRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.GeneralEndpoint, a), Name: "Jane Doe",
				}
			},
		},
		{
			Path:     p(common.SettingsEndpoint, common.TabEndpoint, common.APIKeysEndpoint),
			Template: settingsAPIKeysTemplatePrefix + "tab.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsAPIKeysRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.APIKeysEndpoint, a),
					Keys:                        []*userAPIKey{viewStubAPIKey("Production Key"), viewStubAPIKey("Staging Key")},
					Orgs:                        orgs,
				}
			},
		},
		{
			Path:     p(common.SettingsEndpoint, common.TabEndpoint, common.UsageEndpoint),
			Template: settingsUsageTemplatePrefix + "tab.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsUsageRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.UsageEndpoint, a),
					PropertiesCount:             5,
					OrgsCount:                   2,
					FormsCount:                  5,
					IncludedPropertiesCount:     50,
					IncludedOrgsCount:           10,
					IncludedFormsCount:          10,
					Limit:                       1000000,
				}
			},
		},
		{
			Path:     p(common.SettingsEndpoint, common.TabEndpoint, common.NotificationsEndpoint),
			Template: settingsNotificationsTemplatePrefix + "tab.html",
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &settingsNotificationsRenderContext{
					SettingsCommonRenderContext: settingsCommon(common.NotificationsEndpoint, a),
					WeeklyReport:                true, MonthlyReport: true, ReportEmail: "reports@example.com", UserEmail: "jane@example.com",
				}
			},
		},
		// --- Audit logs ---
		{
			Path:       p(common.AuditLogsEndpoint),
			Template:   auditLogsTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &MainAuditLogsRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					AuditLogsRenderContext: auditLogsCtx,
					From:                   1, To: len(auditLogs), Days: 14,
				}
			},
		},
		// --- Org pages ---
		{
			Path:       p(common.OrgEndpoint, common.NewEndpoint),
			Template:   orgWizardTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &orgWizardRenderContext{CsrfRenderContext: token, AlertRenderContext: a}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg),
			Template:   portalTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &orgDashboardRenderContext{
					portalBaseRenderContext: baseCtx(portalPropertiesTabIndex),
					PaginationRenderContext: pagination,
					Properties:              []*userProperty{prop, viewStubProperty("Blog", "org1")},
					Sort:                    db.OrgPropertiesSortDateAscending,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.PropertiesEndpoint),
			Template: orgPropertiesTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &orgPropertiesRenderContext{
					CsrfRenderContext: token, PaginationRenderContext: pagination,
					Properties: []*userProperty{prop, viewStubProperty("Blog", "org1")},
					CurrentOrg: org,
					Sort:       db.OrgPropertiesSortDateAscending,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.FormsEndpoint),
			Template: orgFormsTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &orgFormsRenderContext{
					portalBaseRenderContext: baseCtx(portalFormsTabIndex),
					PaginationRenderContext: pagination,
					Forms: []*userForm{
						viewStubForm("Contact us", "org1"),
						viewStubForm("Support", "org1"),
					},
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.TabEndpoint, common.FormsEndpoint),
			Template: orgFormsTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &orgFormsRenderContext{
					portalBaseRenderContext: baseCtx(portalFormsTabIndex),
					PaginationRenderContext: pagination,
					Forms: []*userForm{
						viewStubForm("Contact us", "org1"),
						viewStubForm("Support", "org1"),
					},
				}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.RulesEndpoint, common.NewEndpoint),
			Template:   ruleTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &RuleWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					RuleFormData: RuleFormData{
						ConditionProperty: string(dbgen.RuleConditionPropertyCountryCode),
						ConditionOperator: string(dbgen.RuleConditionOperatorIn),
						ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
						Enabled:           true,
					},
					Countries: countries, CurrentOrg: org,
				}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.RulesEndpoint, ruleArg, common.EditEndpoint),
			Template:   ruleTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &RuleWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					RuleFormData: RuleFormData{
						Name:              "Block suspicious countries",
						ConditionProperty: string(dbgen.RuleConditionPropertyCountryCode),
						ConditionOperator: string(dbgen.RuleConditionOperatorIn),
						ConditionValue:    "AB, CD", ActionProperty: string(dbgen.RuleActionPropertyHTTPRequest),
						ActionValue: "0", Enabled: true,
					},
					Countries: countries, CurrentOrg: org, RuleID: "r1", IsEdit: true,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.TabEndpoint, common.DashboardEndpoint),
			Template: orgDashboardTemplate,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &orgDashboardRenderContext{
					portalBaseRenderContext: baseCtx(portalPropertiesTabIndex), PaginationRenderContext: pagination,
					Properties: []*userProperty{prop, viewStubProperty("Blog", "org1")},
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.TabEndpoint, common.MembersEndpoint),
			Template: orgMembersTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &orgMemberRenderContext{
					portalBaseRenderContext: baseCtx(portalMembersTabIndex), AlertRenderContext: a, Members: members,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.TabEndpoint, common.SettingsEndpoint),
			Template: orgSettingsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &orgSettingsRenderContext{
					portalBaseRenderContext: baseCtx(portalSettingsTabIndex), AlertRenderContext: a,
					Members: transferMembers, CanTransfer: true,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.TabEndpoint, common.EventsEndpoint),
			Template: orgAuditLogsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &orgAuditLogsRenderContext{
					portalBaseRenderContext: baseCtx(portalEventsTabIndex), AlertRenderContext: a,
					AuditLogsRenderContext: auditLogsCtx, CanView: true,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.TabEndpoint, common.RulesEndpoint),
			Template: orgRulesTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &OrgRulesRenderContext{
					portalBaseRenderContext: baseCtx(portalRulesTabIndex), AlertRenderContext: a,
					rulesRenderContext: rulesRenderContext{Rules: rules, CanAddNew: true},
				}
			},
		},
		// --- Property pages ---
		{
			Path:       p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, common.NewEndpoint),
			Template:   propertyWizardTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return struct {
					propertyWizardRenderContext
					Property *userProperty
					Sitekey  string
				}{
					propertyWizardRenderContext: propertyWizardRenderContext{
						CsrfRenderContext: token, AlertRenderContext: a, CurrentOrg: org,
					},
					Property: prop,
					Sitekey:  db.TestPropertySitekey,
				}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.FormEndpoint, common.NewEndpoint),
			Template:   formWizardTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &formWizardRenderContext{
					CsrfRenderContext:  token,
					AlertRenderContext: a,
					CurrentOrg:         org,
				}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.FormEndpoint, common.NewEndpoint, "setup"),
			Template:   formWizardTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return struct {
					formIntegrationRenderContext
					Step int
				}{
					formIntegrationRenderContext: formIntegrationRenderContext{
						CsrfRenderContext: token,
						Form:              viewStubForm("Contact us", "org1"),
						CurrentOrg:        org,
						Sitekey:           db.TestPropertySitekey,
					},
					Step: 1,
				}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.FormEndpoint, formArg),
			Template:   formDashboardTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				c := formDash(formReportsTabIndex, a)
				return &c
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.FormEndpoint, formArg, common.TabEndpoint, common.ReportsEndpoint),
			Template: formDashboardReportsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				c := formDash(formReportsTabIndex, a)
				return &c
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.FormEndpoint, formArg, common.TabEndpoint, common.IntegrationsEndpoint),
			Template: formDashboardIntegrationsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &formDashboardIntegrationsRenderContext{
					formDashboardRenderContext: formDash(formIntegrationsTabIndex, a),
					Sitekey:                    db.TestPropertySitekey,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.FormEndpoint, formArg, common.TabEndpoint, common.SettingsEndpoint),
			Template: formDashboardSettingsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &formSettingsRenderContext{
					formDashboardRenderContext: formDash(formSettingsTabIndex, a),
					Orgs:                       orgs,
					CanMove:                    true,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.FormEndpoint, formArg, common.TabEndpoint, common.EventsEndpoint),
			Template: formDashboardAuditLogsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &formAuditLogsRenderContext{
					formDashboardRenderContext: formDash(formAuditLogsTabIndex, a),
					AuditLogsRenderContext:     auditLogsCtx,
					CanView:                    true,
				}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, common.NewEndpoint, "setup"),
			Template:   propertyWizardTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return struct {
					propertyIntegrationsRenderContext
					CurrentOrg *UserOrg
					Step       int
				}{
					propertyIntegrationsRenderContext: propertyIntegrationsRenderContext{
						propertyDashboardRenderContext: propDash(propertyIntegrationsTabIndex, a),
						Sitekey:                        db.TestPropertySitekey,
					},
					CurrentOrg: org,
					Step:       1,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.ClientSetupEndpoint),
			Template: propertyWizardClientSetupTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &propertyIntegrationsRenderContext{
					propertyDashboardRenderContext: propDash(propertyIntegrationsTabIndex, a),
					Sitekey:                        db.TestPropertySitekey,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.ServerSetupEndpoint),
			Template: propertyWizardServerSetupTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &propertyIntegrationsRenderContext{
					propertyDashboardRenderContext: propDash(propertyIntegrationsTabIndex, a),
					Sitekey:                        db.TestPropertySitekey,
				}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg),
			Template:   propertyDashboardTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				c := propDash(propertyReportsTabIndex, a)
				return &c
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.RulesEndpoint, common.NewEndpoint),
			Template:   ruleTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &RuleWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					RuleFormData: RuleFormData{
						ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
						ConditionOperator: string(dbgen.RuleConditionOperatorContains),
						ActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
						Enabled:           true,
					},
					Countries: countries, CurrentOrg: org, Property: viewStubProperty("Main Site", "org1"),
				}
			},
		},
		{
			Path:       p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.RulesEndpoint, ruleArg, common.EditEndpoint),
			Template:   ruleTemplate,
			ShowInList: true,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &RuleWizardRenderContext{
					CsrfRenderContext: token, AlertRenderContext: a,
					RuleFormData: RuleFormData{
						Name:              "Lower difficulty for mobile",
						ConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
						ConditionOperator: string(dbgen.RuleConditionOperatorContains),
						ConditionValue:    "Mobile", ActionProperty: string(dbgen.RuleActionPropertyDifficultyLevelPercent),
						ActionValue: "-20", Enabled: true,
					},
					Countries: countries, CurrentOrg: org, Property: viewStubProperty("Main Site", "org1"),
					RuleID: "r2", IsEdit: true,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.TabEndpoint, common.ReportsEndpoint),
			Template: propertyDashboardReportsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				c := propDash(propertyReportsTabIndex, a)
				return &c
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.TabEndpoint, common.SettingsEndpoint),
			Template: propertyDashboardSettingsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				ctx := &propertySettingsRenderContext{
					propertyDashboardRenderContext: propDash(propertySettingsTabIndex, a),
					difficultyLevelsRenderContext:  createDifficultyLevelsRenderContext(),
					Orgs:                           orgs, MinLevel: int(common.MinDifficultyLevel), MaxLevel: int(common.MaxDifficultyLevel),
					CanMove: true,
				}
				ctx.UpdateLevels()
				return ctx
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.TabEndpoint, common.IntegrationsEndpoint),
			Template: propertyDashboardIntegrationsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &propertyIntegrationsRenderContext{
					propertyDashboardRenderContext: propDash(propertyIntegrationsTabIndex, a),
					Sitekey:                        db.TestPropertySitekey,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.TabEndpoint, common.EventsEndpoint),
			Template: propertyDashboardAuditLogsTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &propertyAuditLogsRenderContext{
					propertyDashboardRenderContext: propDash(propertyAuditLogsTabIndex, a),
					AuditLogsRenderContext:         auditLogsCtx, CanView: true,
				}
			},
		},
		{
			Path:     p(common.OrgEndpoint, orgArg, common.PropertyEndpoint, propArg, common.TabEndpoint, common.RulesEndpoint),
			Template: propertyDashboardRulesTemplate,
			ModelFunc: func(a AlertRenderContext) interface{} {
				return &PropertyRulesRenderContext{
					propertyDashboardRenderContext: propDash(propertyRulesTabIndex, a),
					rulesRenderContext:             rulesRenderContext{Rules: rules, CanAddNew: true},
				}
			},
		},
		// --- Org invite ---
		{
			Path:       p(common.OrgInviteEndpoint, arg(common.ParamID), common.RegisterEndpoint),
			Template:   loginTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				return &loginRenderContext{
					CsrfRenderContext: token, CaptchaRenderContext: captchaCtx,
					Email: "invited@example.com", CanRegister: true, IsRegister: true,
				}
			},
		},
		// --- Root ---
		{
			Path:       s.RelURL("/{$}"),
			Template:   portalTemplate,
			ShowInList: true,
			ModelFunc: func(_ AlertRenderContext) interface{} {
				bctx := baseCtx(portalPropertiesTabIndex)
				bctx.ShowOnboarding = true
				return &orgDashboardRenderContext{
					portalBaseRenderContext: bctx,
					PaginationRenderContext: pagination,
					Properties:              []*userProperty{prop, viewStubProperty("Blog", "org1")},
					Sort:                    db.OrgPropertiesSortDateAscending,
				}
			},
		},
	}
}
