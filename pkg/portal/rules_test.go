package portal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
	"github.com/jackc/pgx/v5/pgtype"
)

func createPropertyRuleUserAgent(ctx context.Context, user *dbgen.User, propertyID int32, name string) (*dbgen.DifficultyRule, error) {
	rule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              name,
		PropertyID:        db.Int(propertyID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("curl"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
	})
	return rule, err
}

func createOrgRuleUserAgent(ctx context.Context, user *dbgen.User, orgID int32, name string) (*dbgen.DifficultyRule, error) {
	rule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              name,
		OrgID:             db.Int(orgID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("curl"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
	})
	return rule, err
}

func createOrgRuleIPAddress(ctx context.Context, user *dbgen.User, orgID int32, name string, enabled bool) (*dbgen.DifficultyRule, error) {
	rule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              name,
		OrgID:             db.Int(orgID),
		Enabled:           enabled,
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: db.Text("10.0.0.0/8"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       20,
	})
	return rule, err
}

func createRuleForMove(ctx context.Context, user *dbgen.User, orgID *int32, propertyID *int32, name string, actionValue int32) (*dbgen.DifficultyRule, error) {
	params := &dbgen.CreateDifficultyRuleParams{
		Name:                     name,
		Enabled:                  true,
		ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator:        dbgen.RuleConditionOperatorContains,
		ConditionOperatorNegated: false,
		ConditionValueStr:        db.Text("test"),
		ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:              actionValue,
		CreatorID:                db.Int(user.ID),
		Column15:                 db.RulePositionStep, // position step for auto-incrementing rule position
	}
	if orgID != nil {
		params.OrgID = db.Int(*orgID)
	} else {
		params.PropertyID = db.Int(*propertyID)
	}
	rule, _, err := server.Store.Impl().CreateDifficultyRule(ctx, user, params)
	return rule, err
}

func postRuleRequest(srv *http.ServeMux, cookie *http.Cookie, method, endpoint, token string, params map[string]string) *http.Response {
	form := url.Values{}
	form.Set(common.ParamCSRFToken, token)
	for key, value := range params {
		form.Set(key, value)
	}

	req := httptest.NewRequest(method, endpoint, strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Result()
}

func postCreatePropertyRule(srv *http.ServeMux, cookie *http.Cookie, user *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, name, conditionValue, actionValue string) *http.Response {
	endpoint := fmt.Sprintf("/org/%s/property/%s/rules/new", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)))
	token := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	params := map[string]string{
		common.ParamName:              name,
		common.ParamEnabled:           "on",
		common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
		common.ParamConditionOperator: string(dbgen.RuleConditionOperatorContains),
		common.ParamConditionValue:    conditionValue,
		common.ParamActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		common.ParamActionValue:       actionValue,
	}
	return postRuleRequest(srv, cookie, "POST", endpoint, token, params)
}

func postCreateOrgRule(srv *http.ServeMux, cookie *http.Cookie, user *dbgen.User, org *dbgen.Organization, name, conditionProp, conditionOp, conditionValue, actionProp, actionValue string) *http.Response {
	endpoint := fmt.Sprintf("/org/%s/rules/new", server.IDHasher.Encrypt(int(org.ID)))
	token := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	params := map[string]string{
		common.ParamName:              name,
		common.ParamEnabled:           "on",
		common.ParamConditionProperty: conditionProp,
		common.ParamConditionOperator: conditionOp,
		common.ParamConditionValue:    conditionValue,
		common.ParamActionProperty:    actionProp,
		common.ParamActionValue:       actionValue,
	}
	return postRuleRequest(srv, cookie, "POST", endpoint, token, params)
}

func postEditPropertyRule(srv *http.ServeMux, cookie *http.Cookie, user *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, rule *dbgen.DifficultyRule, name, conditionOp, conditionValue, actionValue string) *http.Response {
	endpoint := fmt.Sprintf("/org/%s/property/%s/rules/%s/edit", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)), server.IDHasher.Encrypt(int(rule.ID)))
	token := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	params := map[string]string{
		common.ParamName:              name,
		common.ParamEnabled:           "on",
		common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
		common.ParamConditionOperator: conditionOp,
		common.ParamConditionValue:    conditionValue,
		common.ParamActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		common.ParamActionValue:       actionValue,
	}
	return postRuleRequest(srv, cookie, "POST", endpoint, token, params)
}

func postEditOrgRule(srv *http.ServeMux, cookie *http.Cookie, user *dbgen.User, org *dbgen.Organization, rule *dbgen.DifficultyRule, name, conditionProp, conditionOp, conditionValue, actionProp, actionValue string) *http.Response {
	endpoint := fmt.Sprintf("/org/%s/rules/%s/edit", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(rule.ID)))
	token := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	params := map[string]string{
		common.ParamName:              name,
		common.ParamEnabled:           "on",
		common.ParamConditionProperty: conditionProp,
		common.ParamConditionOperator: conditionOp,
		common.ParamConditionValue:    conditionValue,
		common.ParamActionProperty:    actionProp,
		common.ParamActionValue:       actionValue,
	}
	return postRuleRequest(srv, cookie, "POST", endpoint, token, params)
}

func TestDifficultyRuleToDisplayConditionPropertyOverride(t *testing.T) {
	hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "salt"))

	rule := &dbgen.DifficultyRule{
		ID:                1,
		Name:              "Test rule",
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: db.Text("10.0.0.0/8"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       20,
	}

	// without registry, should use default display name
	model := difficultyRuleToDisplay(rule, true, hasher, nil)
	if model.ConditionProperty != "Ip Address" {
		t.Errorf("Expected 'Ip Address', got '%s'", model.ConditionProperty)
	}

	// with registry, should use custom display name
	registry := NewRuleRegistry()
	registry.RegisterCondition(string(dbgen.RuleConditionPropertyIPAddress), nil, "Custom IP Display", nil)
	model = difficultyRuleToDisplay(rule, true, hasher, registry)
	if model.ConditionProperty != "Custom IP Display" {
		t.Errorf("Expected 'Custom IP Display', got '%s'", model.ConditionProperty)
	}

	// unknown property without registry should fall back to title case
	rule.ConditionProperty = "some_custom_property"
	model = difficultyRuleToDisplay(rule, true, hasher, nil)
	if model.ConditionProperty != "Some Custom Property" {
		t.Errorf("Expected 'Some Custom Property', got '%s'", model.ConditionProperty)
	}

	// unknown property with registry should fall back to title case from ConditionDisplayName
	model = difficultyRuleToDisplay(rule, true, hasher, registry)
	if model.ConditionProperty != "Some Custom Property" {
		t.Errorf("Expected 'Some Custom Property', got '%s'", model.ConditionProperty)
	}

	// unknown property with registry override should use custom display name
	registry.RegisterCondition("some_custom_property", nil, "My Custom Prop", nil)
	model = difficultyRuleToDisplay(rule, true, hasher, registry)
	if model.ConditionProperty != "My Custom Prop" {
		t.Errorf("Expected 'My Custom Prop', got '%s'", model.ConditionProperty)
	}
}

func TestFormatConditionValue(t *testing.T) {
	t.Parallel()

	t.Run("NilRegistryWithStringValue", func(t *testing.T) {
		rule := &dbgen.DifficultyRule{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionValueStr: db.Text("Mozilla/5.0"),
		}

		var registry *RuleRegistry
		result := registry.FormatConditionValue(rule)
		if result != "Mozilla/5.0" {
			t.Errorf("Expected 'Mozilla/5.0', got '%s'", result)
		}
	})

	t.Run("NilRegistryWithIntValue", func(t *testing.T) {
		rule := &dbgen.DifficultyRule{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionValueInt: pgtype.Int4{Int32: 42, Valid: true},
		}

		var registry *RuleRegistry
		result := registry.FormatConditionValue(rule)
		if result != "42" {
			t.Errorf("Expected '42', got '%s'", result)
		}
	})

	t.Run("NilRegistryWithNoValue", func(t *testing.T) {
		rule := &dbgen.DifficultyRule{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		}

		var registry *RuleRegistry
		result := registry.FormatConditionValue(rule)
		if result != "" {
			t.Errorf("Expected empty string, got '%s'", result)
		}
	})

	t.Run("RegistryWithoutFormatterFallsBackToString", func(t *testing.T) {
		rule := &dbgen.DifficultyRule{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionValueStr: db.Text("curl/7.0"),
		}

		registry := NewRuleRegistry()
		registry.RegisterCondition(string(dbgen.RuleConditionPropertyUserAgent), nil, "User Agent", nil)
		result := registry.FormatConditionValue(rule)
		if result != "curl/7.0" {
			t.Errorf("Expected 'curl/7.0', got '%s'", result)
		}
	})

	t.Run("RegistryWithoutFormatterFallsBackToInt", func(t *testing.T) {
		rule := &dbgen.DifficultyRule{
			ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
			ConditionValueInt: pgtype.Int4{Int32: 99, Valid: true},
		}

		registry := NewRuleRegistry()
		registry.RegisterCondition(string(dbgen.RuleConditionPropertyUserAgent), nil, "User Agent", nil)
		result := registry.FormatConditionValue(rule)
		if result != "99" {
			t.Errorf("Expected '99', got '%s'", result)
		}
	})

	t.Run("RegistryWithCustomFormatter", func(t *testing.T) {
		rule := &dbgen.DifficultyRule{
			ConditionProperty: "custom_property",
			ConditionValueInt: pgtype.Int4{Int32: 5, Valid: true},
		}

		registry := NewRuleRegistry()
		registry.RegisterCondition("custom_property", nil, "Custom Prop", func(r *dbgen.DifficultyRule) string {
			return fmt.Sprintf("Level %d", r.ConditionValueInt.Int32)
		})
		result := registry.FormatConditionValue(rule)
		if result != "Level 5" {
			t.Errorf("Expected 'Level 5', got '%s'", result)
		}
	})

	t.Run("RegistryWithUnregisteredPropertyFallsBack", func(t *testing.T) {
		rule := &dbgen.DifficultyRule{
			ConditionProperty: "unknown_property",
			ConditionValueStr: db.Text("test-value"),
		}

		registry := NewRuleRegistry()
		result := registry.FormatConditionValue(rule)
		if result != "test-value" {
			t.Errorf("Expected 'test-value', got '%s'", result)
		}
	})

	t.Run("FormatterUsedInDifficultyRuleToDisplay", func(t *testing.T) {
		hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "salt"))
		rule := &dbgen.DifficultyRule{
			ID:                1,
			Name:              "Custom format rule",
			Enabled:           true,
			ConditionProperty: "custom_property",
			ConditionOperator: dbgen.RuleConditionOperatorEquals,
			ConditionValueInt: pgtype.Int4{Int32: 3, Valid: true},
			ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:       20,
		}

		registry := NewRuleRegistry()
		registry.RegisterCondition("custom_property", nil, "Custom Prop", func(r *dbgen.DifficultyRule) string {
			return fmt.Sprintf("option-%d", r.ConditionValueInt.Int32)
		})

		model := difficultyRuleToDisplay(rule, true, hasher, registry)
		if model.ConditionValue != "option-3" {
			t.Errorf("Expected 'option-3', got '%s'", model.ConditionValue)
		}
	})
}

func TestParseUserAgentConditionInvalidOperator(t *testing.T) {
	tests := []struct {
		name     string
		operator string
		value    string
		expected common.StatusCode
	}{
		{
			name:     "invalid operator matches",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "test",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator in",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "test",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "unknown operator",
			operator: "unknown",
			value:    "test",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "empty operator",
			operator: "",
			value:    "test",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "equals with empty value",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "",
			expected: common.StatusRuleConditionValueRequired,
		},
		{
			name:     "contains with empty value",
			operator: string(dbgen.RuleConditionOperatorContains),
			value:    "",
			expected: common.StatusRuleConditionValueRequired,
		},
		{
			name:     "empty operator valid without value",
			operator: string(dbgen.RuleConditionOperatorEmpty),
			value:    "",
			expected: common.StatusOK,
		},
		{
			name:     "bot operator valid without value",
			operator: string(dbgen.RuleConditionOperatorBot),
			value:    "",
			expected: common.StatusOK,
		},
		{
			name:     "equals with value is valid",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "test",
			expected: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, got := userAgentConditionParser(tt.operator, tt.value, "")
			if got != tt.expected {
				t.Errorf("userAgentConditionParser() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseIPAddressConditionInvalidOperator(t *testing.T) {
	tests := []struct {
		name     string
		operator string
		value    string
		expected common.StatusCode
	}{
		{
			name:     "invalid operator equals",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "1.2.3.4",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator contains",
			operator: string(dbgen.RuleConditionOperatorContains),
			value:    "1.2.3.4",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator in",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "1.2.3.4",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "unknown operator",
			operator: "unknown",
			value:    "1.2.3.4",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "matches with empty value",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "",
			expected: common.StatusRuleIPAddressRequired,
		},
		{
			name:     "empty operator valid without value",
			operator: string(dbgen.RuleConditionOperatorEmpty),
			value:    "",
			expected: common.StatusOK,
		},
		{
			name:     "matches with value is valid",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "192.168.0.0/16",
			expected: common.StatusOK,
		},
		{
			name:     "matches with single exact IP is valid",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "1.2.3.4",
			expected: common.StatusOK,
		},
		{
			name:     "matches with comma-separated list of IPs is valid",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "1.2.3.4,5.6.7.8,10.0.0.0/8",
			expected: common.StatusOK,
		},
		{
			name:     "matches with invalid IP in list",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "1.2.3.4,not-an-ip",
			expected: common.StatusRuleIPAddressInvalid,
		},
		{
			name:     "matches with empty entry in list is skipped",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "1.2.3.4,,5.6.7.8",
			expected: common.StatusOK,
		},
		{
			name:     "matches with only commas returns required",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    ",,,",
			expected: common.StatusRuleIPAddressRequired,
		},
		{
			name:     "matches with too many IPs",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "1.0.0.1,1.0.0.2,1.0.0.3,1.0.0.4,1.0.0.5,1.0.0.6,1.0.0.7,1.0.0.8,1.0.0.9,1.0.0.10,1.0.0.11",
			expected: common.StatusRuleIPAddressTooMany,
		},
		{
			name:     "matches with IPv6 address is valid",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "2001:db8::1",
			expected: common.StatusOK,
		},
		{
			name:     "matches with IPv6 prefix is valid",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "2001:db8::/32",
			expected: common.StatusOK,
		},
		{
			name:     "matches with mixed IPv4 and IPv6 list is valid",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "10.0.0.0/8,2001:db8::/32",
			expected: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, got := ipAddressConditionParser(tt.operator, tt.value, "")
			if got != tt.expected {
				t.Errorf("ipAddressConditionParser() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseCountryCodeConditionInvalidOperator(t *testing.T) {
	tests := []struct {
		name     string
		operator string
		value    string
		expected common.StatusCode
	}{
		{
			name:     "invalid operator equals",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "US",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator contains",
			operator: string(dbgen.RuleConditionOperatorContains),
			value:    "US",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator matches",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "US",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator empty",
			operator: string(dbgen.RuleConditionOperatorEmpty),
			value:    "US",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "unknown operator",
			operator: "unknown",
			value:    "US",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "in operator with empty value",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "",
			expected: common.StatusRuleCountryRequired,
		},
		{
			name:     "in operator with value is valid",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "US,GB",
			expected: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, got := countryCodeConditionParser(tt.operator, tt.value, "")
			if got != tt.expected {
				t.Errorf("countryCodeConditionParser() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseDifficultyAction(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		expectedVal  int32
		expectedCode common.StatusCode
	}{
		{
			name:         "empty value",
			value:        "",
			expectedVal:  0,
			expectedCode: common.StatusRuleActionValueRequired,
		},
		{
			name:         "non numeric value",
			value:        "abc",
			expectedVal:  0,
			expectedCode: common.StatusRuleActionValueInvalid,
		},
		{
			name:         "value too low",
			value:        "-301",
			expectedVal:  0,
			expectedCode: common.StatusRuleDifficultyValueInvalid,
		},
		{
			name:         "value too high",
			value:        "301",
			expectedVal:  0,
			expectedCode: common.StatusRuleDifficultyValueInvalid,
		},
		{
			name:         "zero value",
			value:        "0",
			expectedVal:  0,
			expectedCode: common.StatusRuleDifficultyValueInvalid,
		},
		{
			name:         "valid positive value",
			value:        "50",
			expectedVal:  50,
			expectedCode: common.StatusOK,
		},
		{
			name:         "valid negative value",
			value:        "-50",
			expectedVal:  -50,
			expectedCode: common.StatusOK,
		},
		{
			name:         "boundary min value",
			value:        "-300",
			expectedVal:  -300,
			expectedCode: common.StatusOK,
		},
		{
			name:         "boundary max value",
			value:        "300",
			expectedVal:  300,
			expectedCode: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, code := difficultyActionParser(tt.value)
			if code != tt.expectedCode {
				t.Errorf("difficultyActionParser() code = %v, want %v", code, tt.expectedCode)
			}
			if val != tt.expectedVal {
				t.Errorf("difficultyActionParser() val = %v, want %v", val, tt.expectedVal)
			}
		})
	}
}

func TestParseDifficultyGrowthAction(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		expectedVal  int32
		expectedCode common.StatusCode
	}{
		{
			name:         "empty value",
			value:        "",
			expectedVal:  0,
			expectedCode: common.StatusRuleActionValueRequired,
		},
		{
			name:         "non numeric value",
			value:        "abc",
			expectedVal:  0,
			expectedCode: common.StatusRuleActionValueInvalid,
		},
		{
			name:         "value too low",
			value:        "-1",
			expectedVal:  0,
			expectedCode: common.StatusRuleDifficultyGrowthInvalid,
		},
		{
			name:         "value too high",
			value:        "4",
			expectedVal:  0,
			expectedCode: common.StatusRuleDifficultyGrowthInvalid,
		},
		{
			name:         "valid constant growth",
			value:        "0",
			expectedVal:  0,
			expectedCode: common.StatusOK,
		},
		{
			name:         "valid fast growth",
			value:        "3",
			expectedVal:  3,
			expectedCode: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, code := difficultyGrowthActionParser(tt.value)
			if code != tt.expectedCode {
				t.Errorf("difficultyGrowthActionParser() code = %v, want %v", code, tt.expectedCode)
			}
			if val != tt.expectedVal {
				t.Errorf("difficultyGrowthActionParser() val = %v, want %v", val, tt.expectedVal)
			}
		})
	}
}

func TestParseHTTPRequestAction(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		expectedVal  int32
		expectedCode common.StatusCode
	}{
		{
			name:         "empty value returns zero",
			value:        "",
			expectedVal:  0,
			expectedCode: common.StatusOK,
		},
		{
			name:         "non empty value returns one",
			value:        "block",
			expectedVal:  1,
			expectedCode: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, code := httpRequestActionParser(tt.value)
			if code != tt.expectedCode {
				t.Errorf("httpRequestActionParser() code = %v, want %v", code, tt.expectedCode)
			}
			if val != tt.expectedVal {
				t.Errorf("httpRequestActionParser() val = %v, want %v", val, tt.expectedVal)
			}
		})
	}
}

func TestCreatePropertyRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	resp := postCreatePropertyRule(srv, cookie, user, org, prop, "Block crawlers", "curl", "50")
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	batch := map[int32]uint{prop.ID: 1}
	rulesMap, err := store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, batch)
	if err != nil {
		t.Fatalf("Failed to retrieve rules: %v", err)
	}

	rules := rulesMap[prop.ID]
	if len(rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules))
	}

	if rules[0].Name != "Block crawlers" {
		t.Errorf("Unexpected rule name: %s", rules[0].Name)
	}
	if !rules[0].Enabled {
		t.Error("Rule should be enabled")
	}
	if rules[0].ConditionProperty != dbgen.RuleConditionPropertyUserAgent {
		t.Errorf("Unexpected condition property: %s", rules[0].ConditionProperty)
	}
	if rules[0].ActionValue != 50 {
		t.Errorf("Unexpected action value: %d", rules[0].ActionValue)
	}
}

func TestCreateOrgRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	resp := postCreateOrgRule(srv, cookie, user, org, "Block countries",
		string(dbgen.RuleConditionPropertyCountryCode),
		string(dbgen.RuleConditionOperatorIn),
		"US,GB",
		string(dbgen.RuleActionPropertyHTTPRequest),
		"block")
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	batch := map[int32]uint{org.ID: 1}
	rulesMap, err := store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, batch)
	if err != nil {
		t.Fatalf("Failed to retrieve rules: %v", err)
	}

	rules := rulesMap[org.ID]
	if len(rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules))
	}

	if rules[0].Name != "Block countries" {
		t.Errorf("Unexpected rule name: %s", rules[0].Name)
	}
	if rules[0].ConditionProperty != dbgen.RuleConditionPropertyCountryCode {
		t.Errorf("Unexpected condition property: %s", rules[0].ConditionProperty)
	}
	if rules[0].ConditionOperator != dbgen.RuleConditionOperatorIn {
		t.Errorf("Unexpected condition operator: %s", rules[0].ConditionOperator)
	}
}

func TestEditPropertyRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	rule, err := createPropertyRuleUserAgent(ctx, user, prop.ID, "Original Rule")
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	resp := postEditPropertyRule(srv, cookie, user, org, prop, rule, "Updated Rule",
		string(dbgen.RuleConditionOperatorEquals)+"_negated", "bot", "-30")
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	updatedRule, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}

	if updatedRule.Name != "Updated Rule" {
		t.Errorf("Unexpected rule name: %s", updatedRule.Name)
	}
	if updatedRule.ConditionOperator != dbgen.RuleConditionOperatorEquals {
		t.Errorf("Unexpected operator: %s", updatedRule.ConditionOperator)
	}
	if !updatedRule.ConditionOperatorNegated {
		t.Error("Expected condition to be negated")
	}
	if updatedRule.ActionValue != -30 {
		t.Errorf("Unexpected action value: %d", updatedRule.ActionValue)
	}
}

func TestEditOrgRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	rule, err := createOrgRuleIPAddress(ctx, user, org.ID, "Original Org Rule", false)
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	resp := postEditOrgRule(srv, cookie, user, org, rule, "Updated Org Rule",
		string(dbgen.RuleConditionPropertyCountryCode),
		string(dbgen.RuleConditionOperatorIn)+"_negated",
		"DE,FR",
		string(dbgen.RuleActionPropertyHTTPRequest),
		"block")
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	updatedRule, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}

	if updatedRule.Name != "Updated Org Rule" {
		t.Errorf("Unexpected rule name: %s", updatedRule.Name)
	}
	if !updatedRule.Enabled {
		t.Error("Expected rule to be enabled")
	}
	if updatedRule.ConditionProperty != dbgen.RuleConditionPropertyCountryCode {
		t.Errorf("Unexpected condition property: %s", updatedRule.ConditionProperty)
	}
	if !updatedRule.ConditionOperatorNegated {
		t.Error("Expected condition to be negated")
	}
}

func TestNonMemberCannotReadPropertyRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create owner and property
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Create a rule
	_, _, err = store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
		Name:              "Test Rule",
		PropertyID:        db.Int(prop.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(owner.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	// Create non-member user
	nonMember, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_nonmember", testPlan)
	if err != nil {
		t.Fatalf("Failed to create non-member account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, nonMember.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to access property rules - should fail
	req := httptest.NewRequest("GET",
		fmt.Sprintf("/org/%s/property/%s/tab/rules", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID))),
		nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should redirect to error page
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect status for non-member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location != nil && !strings.Contains(location.String(), "error") {
		t.Errorf("Expected redirect to error page, got: %s", location.String())
	}
}

func TestInvitedMemberCannotReadPropertyRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create owner and property
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Create a rule
	_, _, err = store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
		Name:              "Test Rule",
		PropertyID:        db.Int(prop.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(owner.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	// Create member and invite (but not join)
	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member to org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to access property rules - should fail (invited but not joined)
	req := httptest.NewRequest("GET",
		fmt.Sprintf("/org/%s/property/%s?tab=rules", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID))),
		nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should redirect to error page
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect status for invited member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location != nil && !strings.Contains(location.String(), "error") {
		t.Errorf("Expected redirect to error page, got: %s", location.String())
	}
}

func TestNonMemberCannotReadOrgRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create owner and org
	owner, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", owner.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	// Create a rule
	_, _, err = store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
		Name:              "Test Org Rule",
		OrgID:             db.Int(org.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(owner.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	// Create non-member user
	nonMember, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_nonmember", testPlan)
	if err != nil {
		t.Fatalf("Failed to create non-member account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, nonMember.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to access org rules - should fail
	req := httptest.NewRequest("GET",
		fmt.Sprintf("/org/%s?tab=rules", server.IDHasher.Encrypt(int(org.ID))),
		nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should redirect to error page
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect status for non-member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location != nil && !strings.Contains(location.String(), "error") {
		t.Errorf("Expected redirect to error page, got: %s", location.String())
	}
}

func TestInvitedMemberCannotReadOrgRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create owner and org
	owner, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", owner.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	// Create a rule
	_, _, err = store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
		Name:              "Test Org Rule",
		OrgID:             db.Int(org.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(owner.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	// Create member and invite (but not join)
	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member to org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to access org rules - should fail (invited but not joined)
	req := httptest.NewRequest("GET",
		fmt.Sprintf("/org/%s/tab/rules", server.IDHasher.Encrypt(int(org.ID))),
		nil)
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should redirect to error page
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect status for invited member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location != nil && !strings.Contains(location.String(), "error") {
		t.Errorf("Expected redirect to error page, got: %s", location.String())
	}
}

func TestMemberCannotUpdatePropertyRuleCreatedByOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create owner and property
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Owner creates a rule
	rule, _, err := store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
		Name:              "Owner Rule",
		PropertyID:        db.Int(prop.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(owner.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	// Create member and join org
	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member to org: %v", err)
	}

	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatalf("Failed for member to join org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to edit rule - should fail
	resp := postEditPropertyRule(srv, cookie, member, org, prop, rule, "Updated Rule",
		string(dbgen.RuleConditionOperatorEquals), "bot", "-30")
	// Should fail with forbidden
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect status for unauthorized member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location != nil && !strings.Contains(location.String(), "error") {
		t.Errorf("Expected redirect to error page, got: %s", location.String())
	}

	// Verify rule was not updated
	updatedRule, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}
	if updatedRule.Name != "Owner Rule" {
		t.Errorf("Rule was incorrectly updated to: %s", updatedRule.Name)
	}
}

func TestMemberCannotUpdateOrgRuleCreatedByOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create owner and org
	owner, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", owner.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	// Owner creates a rule
	rule, _, err := store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
		Name:              "Owner Org Rule",
		OrgID:             db.Int(org.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(owner.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	// Create member and join org
	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member to org: %v", err)
	}

	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatalf("Failed for member to join org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to edit rule - should fail
	resp := postEditOrgRule(srv, cookie, member, org, rule, "Updated Org Rule",
		string(dbgen.RuleConditionPropertyUserAgent),
		string(dbgen.RuleConditionOperatorEquals),
		"bot",
		string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		"-30")
	// Should fail with forbidden
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect status for unauthorized member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location != nil && !strings.Contains(location.String(), "error") {
		t.Errorf("Expected redirect to error page, got: %s", location.String())
	}

	// Verify rule was not updated
	updatedRule, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}
	if updatedRule.Name != "Owner Org Rule" {
		t.Errorf("Rule was incorrectly updated to: %s", updatedRule.Name)
	}
}

func TestDeletePropertyRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	rule, err := createPropertyRuleUserAgent(ctx, user, prop.ID, "Test Rule")
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	req := httptest.NewRequest("DELETE",
		fmt.Sprintf("/org/%s/property/%s/rules/%s/delete", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)), server.IDHasher.Encrypt(int(rule.ID))),
		nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	// Verify rule is soft deleted
	if _, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID); err == nil {
		t.Fatal("Rule was not deleted")
	}
}

func TestDeleteOrgRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	rule, err := createOrgRuleUserAgent(ctx, user, org.ID, "Test Org Rule")
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(user.ID)))

	req := httptest.NewRequest("DELETE",
		fmt.Sprintf("/org/%s/rules/%s/delete", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(rule.ID))),
		nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Unexpected status code %v", resp.StatusCode)
	}

	// Verify rule is soft deleted
	if _, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID); err == nil {
		t.Fatal("Rule was not deleted")
	}
}

func TestMemberCannotDeletePropertyRuleCreatedByOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create owner and property
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Owner creates a rule
	rule, _, err := store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
		Name:              "Owner Rule",
		PropertyID:        db.Int(prop.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(owner.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	// Create member and join org
	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member to org: %v", err)
	}

	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatalf("Failed for member to join org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(member.ID)))

	// Try to delete rule - should fail
	req := httptest.NewRequest("DELETE",
		fmt.Sprintf("/org/%s/property/%s/rules/%s/delete", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)), server.IDHasher.Encrypt(int(rule.ID))),
		nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should fail with forbidden
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect status for unauthorized member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location != nil && !strings.Contains(location.String(), "error") {
		t.Errorf("Expected redirect to error page, got: %s", location.String())
	}

	// Verify rule was not deleted
	if _, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID); err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}
}

func TestMemberCannotDeleteOrgRuleCreatedByOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create owner and org
	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	// Owner creates a rule
	rule, _, err := store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
		Name:              "Owner Org Rule",
		OrgID:             db.Int(org.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(owner.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	// Create member and join org
	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member to org: %v", err)
	}

	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatalf("Failed for member to join org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	csrfToken := server.XSRF.Token(strconv.Itoa(int(member.ID)))

	// Try to delete rule - should fail
	req := httptest.NewRequest("DELETE",
		fmt.Sprintf("/org/%s/rules/%s/delete", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(rule.ID))),
		nil)
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderCSRFToken, csrfToken)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
	// Should fail with forbidden
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Expected redirect status for unauthorized member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location != nil && !strings.Contains(location.String(), "error") {
		t.Errorf("Expected redirect to error page, got: %s", location.String())
	}

	// Verify rule was not deleted
	if _, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID); err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}
}

// Test suite for property rules with cache testing
func TestRetrievePropertyRulesWithCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tests := []struct {
		name       string
		clearCache bool
	}{
		{
			name:       "with cache",
			clearCache: false,
		},
		{
			name:       "without cache",
			clearCache: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
			if err != nil {
				t.Fatalf("Failed to create account: %v", err)
			}

			prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
			if err != nil {
				t.Fatalf("Failed to create property: %v", err)
			}

			srv := http.NewServeMux()
			server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

			cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
			if err != nil {
				t.Fatal(err)
			}

			// Create multiple rules via POST routes
			rules := []struct {
				name     string
				operator string
				value    string
			}{
				{"Block crawlers", string(dbgen.RuleConditionOperatorContains), "curl"},
				{"Block bots", string(dbgen.RuleConditionOperatorContains), "bot"},
				{"Check Firefox", string(dbgen.RuleConditionOperatorContains), "Firefox"},
			}

			for _, rule := range rules {
				form := url.Values{}
				form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
				form.Set(common.ParamName, rule.name)
				form.Set(common.ParamEnabled, "on")
				form.Set(common.ParamConditionProperty, string(dbgen.RuleConditionPropertyUserAgent))
				form.Set(common.ParamConditionOperator, rule.operator)
				form.Set(common.ParamConditionValue, rule.value)
				form.Set(common.ParamActionProperty, string(dbgen.RuleActionPropertyDifficultyLevelPercent))
				form.Set(common.ParamActionValue, "50")

				req := httptest.NewRequest("POST",
					fmt.Sprintf("/org/%s/property/%s/rules/new", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID))),
					strings.NewReader(form.Encode()))
				req.AddCookie(cookie)
				req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

				w := httptest.NewRecorder()
				srv.ServeHTTP(w, req)

				resp := w.Result()
				if resp.StatusCode != http.StatusSeeOther {
					t.Fatalf("Failed to create rule %s: status code %v", rule.name, resp.StatusCode)
				}
			}

			// Clear cache if requested
			if tt.clearCache {
				_ = cache.Delete(ctx, db.RawPropertyRulesCacheKey(prop.ID))
			}

			// Read rules via server's CreatePropertyRulesContext method
			req := httptest.NewRequest("GET",
				fmt.Sprintf("/org/%s/property/%s/%s/%s", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)), common.TabEndpoint, common.RulesEndpoint),
				nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Failed to get property rules: status code %v", resp.StatusCode)
			}

			// Verify rules via direct retrieval
			batch := map[int32]uint{prop.ID: 1}
			rulesMap, err := store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, batch)
			if err != nil {
				t.Fatalf("Failed to retrieve rules: %v", err)
			}

			retrievedRules := rulesMap[prop.ID]
			if len(retrievedRules) != len(rules) {
				t.Fatalf("Expected %d rules, got %d", len(rules), len(retrievedRules))
			}

			// Verify all rules are present using a map-based comparison
			expectedNames := make(map[string]bool)
			for _, rule := range rules {
				expectedNames[rule.name] = true
			}

			for _, retrievedRule := range retrievedRules {
				if !expectedNames[retrievedRule.Name] {
					t.Errorf("Unexpected rule name: %s", retrievedRule.Name)
				}
				if !retrievedRule.Enabled {
					t.Errorf("Rule %s should be enabled", retrievedRule.Name)
				}
				if retrievedRule.ConditionProperty != dbgen.RuleConditionPropertyUserAgent {
					t.Errorf("Rule %s: unexpected condition property: %s", retrievedRule.Name, retrievedRule.ConditionProperty)
				}
				delete(expectedNames, retrievedRule.Name)
			}

			if len(expectedNames) > 0 {
				t.Errorf("Missing rules: %v", expectedNames)
			}
		})
	}
}

// Test suite for org rules with cache testing
func TestRetrieveOrgRulesWithCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tests := []struct {
		name       string
		clearCache bool
	}{
		{
			name:       "with cache",
			clearCache: false,
		},
		{
			name:       "without cache",
			clearCache: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
			if err != nil {
				t.Fatalf("Failed to create account: %v", err)
			}

			org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", user.ID)
			if err != nil {
				t.Fatalf("Failed to create org: %v", err)
			}

			srv := http.NewServeMux()
			server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

			cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
			if err != nil {
				t.Fatal(err)
			}

			// Create multiple rules via POST routes
			rules := []struct {
				name     string
				operator string
				value    string
			}{
				{"Block US", string(dbgen.RuleConditionOperatorIn), "US"},
				{"Block GB", string(dbgen.RuleConditionOperatorIn), "GB"},
				{"Block CA", string(dbgen.RuleConditionOperatorIn), "CA"},
			}

			for _, rule := range rules {
				form := url.Values{}
				form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
				form.Set(common.ParamName, rule.name)
				form.Set(common.ParamEnabled, "on")
				form.Set(common.ParamConditionProperty, string(dbgen.RuleConditionPropertyCountryCode))
				form.Set(common.ParamConditionOperator, rule.operator)
				form.Set(common.ParamConditionValue, rule.value)
				form.Set(common.ParamActionProperty, string(dbgen.RuleActionPropertyHTTPRequest))
				form.Set(common.ParamActionValue, "block")

				req := httptest.NewRequest("POST",
					fmt.Sprintf("/org/%s/rules/new", server.IDHasher.Encrypt(int(org.ID))),
					strings.NewReader(form.Encode()))
				req.AddCookie(cookie)
				req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

				w := httptest.NewRecorder()
				srv.ServeHTTP(w, req)

				resp := w.Result()
				if resp.StatusCode != http.StatusSeeOther {
					t.Fatalf("Failed to create rule %s: status code %v", rule.name, resp.StatusCode)
				}
			}

			// Clear cache if requested
			if tt.clearCache {
				_ = cache.Delete(ctx, db.RawOrgRulesCacheKey(org.ID))
			}

			// Read rules via server's CreateOrgRulesContext method
			req := httptest.NewRequest("GET",
				fmt.Sprintf("/org/%s?tab=rules", server.IDHasher.Encrypt(int(org.ID))),
				nil)
			req.AddCookie(cookie)

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Failed to get org rules: status code %v", resp.StatusCode)
			}

			// Verify rules via direct retrieval
			batch := map[int32]uint{org.ID: 1}
			rulesMap, err := store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, batch)
			if err != nil {
				t.Fatalf("Failed to retrieve rules: %v", err)
			}

			retrievedRules := rulesMap[org.ID]
			if len(retrievedRules) != len(rules) {
				t.Fatalf("Expected %d rules, got %d", len(rules), len(retrievedRules))
			}

			// Verify all rules are present using a map-based comparison
			expectedNames := make(map[string]bool)
			for _, rule := range rules {
				expectedNames[rule.name] = true
			}

			for _, retrievedRule := range retrievedRules {
				if !expectedNames[retrievedRule.Name] {
					t.Errorf("Unexpected rule name: %s", retrievedRule.Name)
				}
				if !retrievedRule.Enabled {
					t.Errorf("Rule %s should be enabled", retrievedRule.Name)
				}
				if retrievedRule.ConditionProperty != dbgen.RuleConditionPropertyCountryCode {
					t.Errorf("Rule %s: unexpected condition property: %s", retrievedRule.Name, retrievedRule.ConditionProperty)
				}
				delete(expectedNames, retrievedRule.Name)
			}

			if len(expectedNames) > 0 {
				t.Errorf("Missing rules: %v", expectedNames)
			}
		})
	}
}

type moveRulesTestSuite struct {
	createRules   func(ctx context.Context, user *dbgen.User, org *dbgen.Organization, numRules int) ([]*dbgen.DifficultyRule, *dbgen.Property, error)
	retrieveRules func(ctx context.Context, org *dbgen.Organization, property *dbgen.Property) ([]*dbgen.DifficultyRule, error)
	moveHandler   func(w http.ResponseWriter, r *http.Request) (*ViewModel, error)
}

func moveElement[T any](slice []T, i, j int) []T {
	element := slice[i]

	// If moving to the right
	if i < j {
		copy(slice[i:j], slice[i+1:j+1])
	} else {
		// If moving to the left
		copy(slice[j+1:i+1], slice[j:i])
	}

	slice[j] = element

	return slice
}

func testMoveRulesSuite(t *testing.T, suite moveRulesTestSuite) {
	for _, numRules := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("%dRules", numRules), func(t *testing.T) {
			// Test moving each rule to each position
			for fromIndex := 0; fromIndex < numRules; fromIndex++ {
				for toIndex := 0; toIndex < numRules; toIndex++ {
					t.Run(fmt.Sprintf("From%dTo%d", fromIndex, toIndex), func(t *testing.T) {
						ctx := t.Context()
						user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
						if err != nil {
							t.Fatalf("Failed to create account: %v", err)
						}

						// Create rules using suite-specific function
						rules, property, err := suite.createRules(ctx, user, org, numRules)
						if err != nil {
							t.Fatal(err)
						}

						srv := http.NewServeMux()
						server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
						cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
						if err != nil {
							t.Fatal(err)
						}

						form := url.Values{}
						form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
						form.Add(common.ParamPosition, strconv.Itoa(toIndex))

						req := httptest.NewRequest("POST", "/endpoint", strings.NewReader(form.Encode()))
						req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
						req.AddCookie(cookie)
						req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
						if property != nil {
							req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
						}
						req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rules[fromIndex].ID)))

						w := httptest.NewRecorder()
						if _, err := suite.moveHandler(w, req); err != nil {
							t.Fatal(err)
						}

						// Verify rule moved to correct position
						movedRules, err := suite.retrieveRules(ctx, org, property)
						if err != nil {
							t.Fatal(err)
						}
						if len(movedRules) != numRules {
							t.Fatalf("Expected %d rules, got %d", numRules, len(movedRules))
						}
						moveElement(rules, fromIndex, toIndex)
						for i := 0; i < numRules; i++ {
							if rules[i].ID != movedRules[i].ID {
								t.Errorf("Expected rule at index %d to be rule %d, got rule %d", i, rules[i].ID, movedRules[i].ID)
							}
						}
					})
				}
			}
		})
	}
}

func TestMovePropertyRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	suite := moveRulesTestSuite{
		createRules: func(ctx context.Context, user *dbgen.User, org *dbgen.Organization, numRules int) ([]*dbgen.DifficultyRule, *dbgen.Property, error) {
			property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "test.com"), org)
			if err != nil {
				return nil, nil, err
			}

			rules := make([]*dbgen.DifficultyRule, numRules)
			for i := 0; i < numRules; i++ {
				rule, err := createRuleForMove(ctx, user, nil, &property.ID, fmt.Sprintf("Test Rule %d", i), int32(10+i))
				if err != nil {
					return nil, nil, err
				}
				rules[i] = rule
			}
			return rules, property, nil
		},
		retrieveRules: func(ctx context.Context, org *dbgen.Organization, property *dbgen.Property) ([]*dbgen.DifficultyRule, error) {
			allRules, err := server.Store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, map[int32]uint{property.ID: 0})
			if err != nil {
				return nil, err
			}
			return allRules[property.ID], nil
		},
		moveHandler: server.postMovePropertyRule,
	}

	testMoveRulesSuite(t, suite)
}

func TestMoveOrgRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	suite := moveRulesTestSuite{
		createRules: func(ctx context.Context, user *dbgen.User, org *dbgen.Organization, numRules int) ([]*dbgen.DifficultyRule, *dbgen.Property, error) {
			rules := make([]*dbgen.DifficultyRule, numRules)
			for i := 0; i < numRules; i++ {
				rule, err := createRuleForMove(ctx, user, &org.ID, nil, fmt.Sprintf("Test Org Rule %d", i), int32(10+i))
				if err != nil {
					return nil, nil, err
				}
				rules[i] = rule
			}
			return rules, nil, nil
		},
		retrieveRules: func(ctx context.Context, org *dbgen.Organization, property *dbgen.Property) ([]*dbgen.DifficultyRule, error) {
			allRules, err := server.Store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{org.ID: 0})
			if err != nil {
				return nil, err
			}
			return allRules[org.ID], nil
		},
		moveHandler: server.postMoveOrgRule,
	}

	testMoveRulesSuite(t, suite)
}

func testCircularMoveOrgRulesSuite(t *testing.T, moveToLast bool) {
	for _, numRules := range []int{2, 3} {
		numRules := numRules
		t.Run(fmt.Sprintf("%dRules", numRules), func(t *testing.T) {
			ctx := t.Context()
			user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
			if err != nil {
				t.Fatalf("Failed to create account: %v", err)
			}

			// Create rules
			rules := make([]*dbgen.DifficultyRule, numRules)
			for i := 0; i < numRules; i++ {
				rule, err := createRuleForMove(ctx, user, &org.ID, nil, fmt.Sprintf("Test Org Rule %d", i), int32(10+i))
				if err != nil {
					t.Fatal(err)
				}
				rules[i] = rule
			}

			// Store original order
			originalRuleIDs := make([]int32, numRules)
			for i := 0; i < numRules; i++ {
				originalRuleIDs[i] = rules[i].ID
			}

			// Set up HTTP server and authentication
			srv := http.NewServeMux()
			server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)
			cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
			if err != nil {
				t.Fatal(err)
			}

			sourcePosition := numRules - 1
			targetPosition := 0
			if moveToLast {
				sourcePosition = 0
				targetPosition = numRules - 1
			}

			assertRuleOrder := func(moveCount int) {
				currentRules, err := server.Store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{org.ID: 0})
				if err != nil {
					t.Fatal(err)
				}
				if len(currentRules[org.ID]) != numRules {
					t.Fatalf("Expected %d rules, got %d", numRules, len(currentRules[org.ID]))
				}

				for i := 0; i < numRules; i++ {
					if currentRules[org.ID][i].ID != originalRuleIDs[i] {
						t.Errorf("After %d circular moves, rule at index %d should be %d but got %d",
							moveCount, i, originalRuleIDs[i], currentRules[org.ID][i].ID)
					}
				}
			}

			// Move one end rule to the opposite end 2*N times and verify the order
			// returns to the original state each time we complete another full cycle.
			for moveNum := 1; moveNum <= 2*numRules; moveNum++ {
				// Get current rules order
				currentRules, err := server.Store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{org.ID: 0})
				if err != nil {
					t.Fatal(err)
				}
				if len(currentRules[org.ID]) != numRules {
					t.Fatalf("Expected %d rules, got %d", numRules, len(currentRules[org.ID]))
				}

				// Find the selected rule's ID
				selectedRuleID := currentRules[org.ID][sourcePosition].ID

				// Move selected rule to target position
				form := url.Values{}
				form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
				form.Add(common.ParamPosition, strconv.Itoa(targetPosition))

				req := httptest.NewRequest("POST", "/endpoint", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(cookie)
				req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
				req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(selectedRuleID)))

				w := httptest.NewRecorder()
				if _, err := server.postMoveOrgRule(w, req); err != nil {
					t.Fatalf("Move %d failed: %v", moveNum, err)
				}

				t.Logf("Move %d: Moved rule %d to position %d", moveNum, selectedRuleID, targetPosition)

				if moveNum%numRules == 0 {
					assertRuleOrder(moveNum)
				}
			}

			t.Logf("Successfully verified circular moves for %d rules after %d moves", numRules, 2*numRules)
		})
	}
}

func TestCircularMoveOrgRulesFirstPosition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	testCircularMoveOrgRulesSuite(t, false)
}

func TestCircularMoveOrgRulesLastPosition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	testCircularMoveOrgRulesSuite(t, true)
}

func TestRebalancingPropertyRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create property
	property, _, err := server.Store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(user.ID, "test.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	// Create multiple rules
	rules := make([]*dbgen.DifficultyRule, 5)
	for i := 0; i < 5; i++ {
		rule, err := createRuleForMove(ctx, user, nil, &property.ID, fmt.Sprintf("Test Rule %d", i), int32(10+i))
		if err != nil {
			t.Fatalf("Failed to create rule %d: %v", i, err)
		}
		rules[i] = rule
	}

	// Corrupt positions to force rebalancing
	if err := db_tests.CorruptDifficultyRulePositionsForTest(ctx, store, &property.ID, nil); err != nil {
		t.Fatalf("Failed to corrupt positions: %v", err)
	}

	// Set up HTTP server and authentication
	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to move a rule - this should trigger rebalancing
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Add(common.ParamPosition, "1")

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/%s/rules/%s/move",
		server.IDHasher.Encrypt(int(org.ID)),
		server.IDHasher.Encrypt(int(property.ID)),
		server.IDHasher.Encrypt(int(rules[2].ID))), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rules[2].ID)))

	w := httptest.NewRecorder()

	// Call through HTTP server
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Move handler returned unexpected status: %d: %s", w.Code, w.Body.String())
	}

	// Verify rules are now properly spaced
	allRules, err := server.Store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, map[int32]uint{property.ID: 0})
	if err != nil {
		t.Fatalf("Failed to retrieve rules: %v", err)
	}
	propertyRules := allRules[property.ID]
	if len(propertyRules) != 5 {
		t.Fatalf("Expected 5 rules, got %d", len(propertyRules))
	}

	// Verify positions are properly spaced (at least 50.0 apart after rebalancing with step=100)
	for i := 1; i < len(propertyRules); i++ {
		delta := propertyRules[i].Position - propertyRules[i-1].Position
		if delta < db.RulePositionStep/2 {
			t.Errorf("Rules %d and %d are too close: delta = %f", i-1, i, delta)
		}
	}

	// Verify the moved rule is at position 1
	if propertyRules[1].ID != rules[2].ID {
		t.Errorf("Expected rule at index 1 to be rule 2, got rule %d", propertyRules[1].ID)
	}
}

func TestTrialPlanOrgRulesLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Create 10 org rules (should succeed since limit is 10 for trial plan)
	for i := 0; i < 10; i++ {
		resp := postCreateOrgRule(srv, cookie, user, org,
			fmt.Sprintf("Org Rule %d", i),
			string(dbgen.RuleConditionPropertyUserAgent),
			string(dbgen.RuleConditionOperatorContains),
			fmt.Sprintf("test-%d", i),
			string(dbgen.RuleActionPropertyDifficultyLevelPercent),
			"10")

		if resp.StatusCode != http.StatusSeeOther {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Failed to create org rule %d: status code %v, body: %s", i, resp.StatusCode, string(body))
		}
	}

	// Verify 10 org rules were created
	orgRules, err := store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{org.ID: 0})
	if err != nil {
		t.Fatalf("Failed to retrieve org rules: %v", err)
	}
	if len(orgRules[org.ID]) != 10 {
		t.Fatalf("Expected 10 org rules, got %d", len(orgRules[org.ID]))
	}

	// Try to create 11th org rule (should fail)
	resp := postCreateOrgRule(srv, cookie, user, org,
		"Org Rule 11",
		string(dbgen.RuleConditionPropertyUserAgent),
		string(dbgen.RuleConditionOperatorContains),
		"test-11",
		string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		"10")

	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("Expected org rule creation to fail due to limit, but it succeeded")
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), common.StatusOrgRulesLimitError.String()) {
		t.Errorf("Expected error message about org rules limit, got: %s", string(body))
	}
}

func TestTrialPlanPropertyRulesLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Create 10 property rules (should succeed since limit is 10 for trial plan)
	for i := 0; i < 10; i++ {
		resp := postCreatePropertyRule(srv, cookie, user, org, property,
			fmt.Sprintf("Property Rule %d", i),
			fmt.Sprintf("test-%d", i),
			"10")

		if resp.StatusCode != http.StatusSeeOther {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Failed to create property rule %d: status code %v, body: %s", i, resp.StatusCode, string(body))
		}
	}

	// Verify 10 property rules were created
	propRules, err := store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, map[int32]uint{property.ID: 0})
	if err != nil {
		t.Fatalf("Failed to retrieve property rules: %v", err)
	}
	if len(propRules[property.ID]) != 10 {
		t.Fatalf("Expected 10 property rules, got %d", len(propRules[property.ID]))
	}

	// Try to create 11th property rule (should fail)
	resp := postCreatePropertyRule(srv, cookie, user, org, property,
		"Property Rule 11",
		"test-11",
		"10")

	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("Expected property rule creation to fail due to limit, but it succeeded")
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), common.StatusPropertyRulesLimitError.String()) {
		t.Errorf("Expected error message about property rules limit, got: %s", string(body))
	}
}

func TestOrgRuleCreationWithoutSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create account without subscription
	user, org, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name(), nil)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to create org rule without subscription (should fail)
	resp := postCreateOrgRule(srv, cookie, user, org,
		"Test Rule",
		string(dbgen.RuleConditionPropertyUserAgent),
		string(dbgen.RuleConditionOperatorContains),
		"test",
		string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		"10")

	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("Expected org rule creation to fail without subscription, but it succeeded")
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), common.StatusOrgRulesSubscriptionRequired.String()) {
		t.Errorf("Expected error message about subscription required, got: %s", string(body))
	}
}

func TestPropertyRuleCreationWithoutSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	// Create account without subscription
	user, org, err := db_tests.CreateNewAccountForTestEx(ctx, store, t.Name(), nil)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to create property rule without subscription (should fail)
	resp := postCreatePropertyRule(srv, cookie, user, org, property,
		"Test Rule",
		"test",
		"10")

	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("Expected property rule creation to fail without subscription, but it succeeded")
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), common.StatusPropertyRulesSubscriptionRequired.String()) {
		t.Errorf("Expected error message about subscription required, got: %s", string(body))
	}
}

func TestEditPropertyRuleEnableOverLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	property, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Create 10 property rules (hitting the trial plan limit)
	for i := 0; i < 10; i++ {
		resp := postCreatePropertyRule(srv, cookie, user, org, property,
			fmt.Sprintf("Property Rule %d", i),
			fmt.Sprintf("test-%d", i),
			"10")

		if resp.StatusCode != http.StatusSeeOther {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Failed to create property rule %d: status code %v, body: %s", i, resp.StatusCode, string(body))
		}
	}

	// Create a disabled rule directly via DB (bypassing portal limits)
	disabledRule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              "Disabled Rule",
		PropertyID:        db.Int(property.ID),
		Enabled:           false,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("disabled-test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(user.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create disabled rule: %v", err)
	}

	// Try to edit the disabled rule to enable it (should fail due to limit)
	resp := postEditPropertyRule(srv, cookie, user, org, property, disabledRule,
		"Disabled Rule Updated",
		string(dbgen.RuleConditionOperatorContains),
		"disabled-test",
		"50")

	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("Expected property rule enable to fail due to limit, but it succeeded")
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), common.StatusPropertyRulesLimitError.String()) {
		t.Errorf("Expected error message about property rules limit, got: %s", string(body))
	}

	// Verify the rule is still disabled
	updatedRule, err := store.Impl().RetrieveDifficultyRule(ctx, disabledRule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}
	if updatedRule.Enabled {
		t.Error("Expected rule to still be disabled after failed enable attempt")
	}
}

func TestEditOrgRuleEnableOverLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Create 10 org rules (hitting the trial plan limit)
	for i := 0; i < 10; i++ {
		resp := postCreateOrgRule(srv, cookie, user, org,
			fmt.Sprintf("Org Rule %d", i),
			string(dbgen.RuleConditionPropertyUserAgent),
			string(dbgen.RuleConditionOperatorContains),
			fmt.Sprintf("test-%d", i),
			string(dbgen.RuleActionPropertyDifficultyLevelPercent),
			"10")

		if resp.StatusCode != http.StatusSeeOther {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Failed to create org rule %d: status code %v, body: %s", i, resp.StatusCode, string(body))
		}
	}

	// Create a disabled org rule directly via DB (bypassing portal limits)
	disabledRule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              "Disabled Org Rule",
		OrgID:             db.Int(org.ID),
		Enabled:           false,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("disabled-test"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
		CreatorID:         db.Int(user.ID),
	})
	if err != nil {
		t.Fatalf("Failed to create disabled rule: %v", err)
	}

	// Try to edit the disabled org rule to enable it (should fail due to limit)
	resp := postEditOrgRule(srv, cookie, user, org, disabledRule,
		"Disabled Org Rule Updated",
		string(dbgen.RuleConditionPropertyUserAgent),
		string(dbgen.RuleConditionOperatorContains),
		"disabled-test",
		string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		"50")

	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("Expected org rule enable to fail due to limit, but it succeeded")
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), common.StatusOrgRulesLimitError.String()) {
		t.Errorf("Expected error message about org rules limit, got: %s", string(body))
	}

	// Verify the rule is still disabled
	updatedRule, err := store.Impl().RetrieveDifficultyRule(ctx, disabledRule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}
	if updatedRule.Enabled {
		t.Error("Expected rule to still be disabled after failed enable attempt")
	}
}

func TestCreateOrgRuleWithDifficultyGrowth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Test creating rule with difficulty growth action (bug #1 fix validation)
	resp := postCreateOrgRule(srv, cookie, user, org, "Growth rule",
		string(dbgen.RuleConditionPropertyUserAgent),
		string(dbgen.RuleConditionOperatorContains),
		"bot",
		string(dbgen.RuleActionPropertyDifficultyGrowth),
		"2") // Medium growth
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status code %v, body: %s", resp.StatusCode, string(body))
	}

	batch := map[int32]uint{org.ID: 1}
	rulesMap, err := store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, batch)
	if err != nil {
		t.Fatalf("Failed to retrieve rules: %v", err)
	}

	rules := rulesMap[org.ID]
	if len(rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules))
	}

	if rules[0].Name != "Growth rule" {
		t.Errorf("Unexpected rule name: %s", rules[0].Name)
	}
	if rules[0].ActionProperty != dbgen.RuleActionPropertyDifficultyGrowth {
		t.Errorf("Unexpected action property: %s", rules[0].ActionProperty)
	}
	if rules[0].ActionValue != 2 {
		t.Errorf("Unexpected action value: %d, expected 2", rules[0].ActionValue)
	}
}

func TestCreatePropertyRuleWithAllActionTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Test 1: Difficulty Level action
	endpoint1 := fmt.Sprintf("/org/%s/property/%s/rules/new", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)))
	token1 := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	params1 := map[string]string{
		common.ParamName:              "Difficulty Level Rule",
		common.ParamEnabled:           "on",
		common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
		common.ParamConditionOperator: string(dbgen.RuleConditionOperatorContains),
		common.ParamConditionValue:    "test",
		common.ParamActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		common.ParamActionValue:       "50",
	}
	resp1 := postRuleRequest(srv, cookie, "POST", endpoint1, token1, params1)
	if resp1.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("Failed to create difficulty level rule: %v, body: %s", resp1.StatusCode, string(body))
	}

	// Test 2: Difficulty Growth action (bug #1 fix validation)
	endpoint2 := fmt.Sprintf("/org/%s/property/%s/rules/new", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)))
	token2 := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	params2 := map[string]string{
		common.ParamName:              "Growth Rule",
		common.ParamEnabled:           "on",
		common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
		common.ParamConditionOperator: string(dbgen.RuleConditionOperatorEquals),
		common.ParamConditionValue:    "crawler",
		common.ParamActionProperty:    string(dbgen.RuleActionPropertyDifficultyGrowth),
		common.ParamActionValue:       "3", // Fast growth
	}
	resp2 := postRuleRequest(srv, cookie, "POST", endpoint2, token2, params2)
	if resp2.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("Failed to create growth rule: %v, body: %s", resp2.StatusCode, string(body))
	}

	// Test 3: HTTP Request Block action
	endpoint3 := fmt.Sprintf("/org/%s/property/%s/rules/new", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)))
	token3 := server.XSRF.Token(strconv.Itoa(int(user.ID)))
	params3 := map[string]string{
		common.ParamName:              "Block Rule",
		common.ParamEnabled:           "on",
		common.ParamConditionProperty: string(dbgen.RuleConditionPropertyIPAddress),
		common.ParamConditionOperator: string(dbgen.RuleConditionOperatorMatches),
		common.ParamConditionValue:    "192.168.0.0/16",
		common.ParamActionProperty:    string(dbgen.RuleActionPropertyHTTPRequest),
		common.ParamActionValue:       "1",
	}
	resp3 := postRuleRequest(srv, cookie, "POST", endpoint3, token3, params3)
	if resp3.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp3.Body)
		t.Fatalf("Failed to create HTTP request rule: %v, body: %s", resp3.StatusCode, string(body))
	}

	// Verify all three rules were created correctly
	batch := map[int32]uint{prop.ID: 3}
	rulesMap, err := store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, batch)
	if err != nil {
		t.Fatalf("Failed to retrieve rules: %v", err)
	}

	rules := rulesMap[prop.ID]
	if len(rules) != 3 {
		t.Fatalf("Expected 3 rules, got %d", len(rules))
	}

	// Find and verify each rule
	var foundDifficulty, foundGrowth, foundBlock bool
	for _, rule := range rules {
		switch rule.ActionProperty {
		case dbgen.RuleActionPropertyDifficultyLevelPercent:
			foundDifficulty = true
			if rule.ActionValue != 50 {
				t.Errorf("Unexpected difficulty value: %d, expected 50", rule.ActionValue)
			}
		case dbgen.RuleActionPropertyDifficultyGrowth:
			foundGrowth = true
			if rule.ActionValue != 3 {
				t.Errorf("Unexpected growth value: %d, expected 3", rule.ActionValue)
			}
		case dbgen.RuleActionPropertyHTTPRequest:
			foundBlock = true
			if rule.ActionValue != 1 {
				t.Errorf("Unexpected block value: %d, expected 1", rule.ActionValue)
			}
		}
	}

	if !foundDifficulty {
		t.Fatal("Difficulty level rule not found")
	}
	if !foundGrowth {
		t.Fatal("Growth rule not found")
	}
	if !foundBlock {
		t.Fatal("Block rule not found")
	}
}

func TestParseDomainCondition(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		operator string
		value    string
		expected common.StatusCode
	}{
		{
			name:     "empty domain not supported",
			domain:   "",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "example.com",
			expected: common.StatusRuleConditionPropertyInvalid,
		},
		{
			name:     "invalid operator matches",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "example.com",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator in",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "example.com",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "unknown operator",
			domain:   "example.com",
			operator: "unknown",
			value:    "example.com",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "equals with empty value",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "",
			expected: common.StatusRuleDomainRequired,
		},
		{
			name:     "contains with empty value",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorContains),
			value:    "",
			expected: common.StatusRuleDomainRequired,
		},
		{
			name:     "domain not a subdomain of property domain",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "other.com",
			expected: common.StatusRuleDomainSubdomain,
		},
		{
			name:     "empty operator valid without value",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorEmpty),
			value:    "",
			expected: common.StatusOK,
		},
		{
			name:     "equals with same domain is valid",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "example.com",
			expected: common.StatusOK,
		},
		{
			name:     "equals with subdomain is valid",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "sub.example.com",
			expected: common.StatusOK,
		},
		{
			name:     "contains with valid domain",
			domain:   "example.com",
			operator: string(dbgen.RuleConditionOperatorContains),
			value:    "example.com",
			expected: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, got := domainConditionParser(tt.operator, tt.value, tt.domain)
			if got != tt.expected {
				t.Errorf("domainConditionParser() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseHTTPHeaderNameCondition(t *testing.T) {
	tests := []struct {
		name     string
		operator string
		value    string
		expected common.StatusCode
	}{
		{
			name:     "invalid operator equals",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "X-Custom-Header",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator contains",
			operator: string(dbgen.RuleConditionOperatorContains),
			value:    "X-Custom-Header",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator matches",
			operator: string(dbgen.RuleConditionOperatorMatches),
			value:    "X-Custom-Header",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "invalid operator empty",
			operator: string(dbgen.RuleConditionOperatorEmpty),
			value:    "",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "unknown operator",
			operator: "unknown",
			value:    "X-Custom-Header",
			expected: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name:     "in with empty value",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "",
			expected: common.StatusRuleHTTPHeaderNameRequired,
		},
		{
			name:     "in with single valid header",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "X-Custom-Header",
			expected: common.StatusOK,
		},
		{
			name:     "in with multiple valid headers",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "X-Header-A,X-Header-B",
			expected: common.StatusOK,
		},
		{
			name:     "in with invalid header name",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    "Invalid Header!",
			expected: common.StatusRuleHTTPHeaderNameInvalid,
		},
		{
			name:     "in with only commas returns required",
			operator: string(dbgen.RuleConditionOperatorIn),
			value:    ",,,",
			expected: common.StatusRuleHTTPHeaderNameRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, got := httpHeaderNameConditionParser(tt.operator, tt.value, "")
			if got != tt.expected {
				t.Errorf("httpHeaderNameConditionParser() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetPropertyNewRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/rules/new", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(prop.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getPropertyNewRule(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != ruleTemplate {
		t.Errorf("Expected view to be %s, got %s", ruleTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*RuleWizardRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *RuleWizardRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.CurrentOrg == nil {
		t.Fatal("Expected CurrentOrg to be populated, got nil")
	}

	if renderCtx.CurrentOrg.ID != server.IDHasher.Encrypt(int(org.ID)) {
		t.Errorf("Expected org ID %s, got %s", server.IDHasher.Encrypt(int(org.ID)), renderCtx.CurrentOrg.ID)
	}

	if renderCtx.Property == nil {
		t.Fatal("Expected Property to be populated, got nil")
	}

	if renderCtx.Property.Domain != prop.Domain {
		t.Errorf("Expected property domain %s, got %s", prop.Domain, renderCtx.Property.Domain)
	}

	if len(renderCtx.Token) == 0 {
		t.Error("Expected CSRF token to be populated")
	}
}

func TestGetOrgNewRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/rules/new", server.IDHasher.Encrypt(int(org.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getOrgNewRule(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != ruleTemplate {
		t.Errorf("Expected view to be %s, got %s", ruleTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*RuleWizardRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *RuleWizardRenderContext, got %T", viewModel.Model)
	}

	if renderCtx.CurrentOrg == nil {
		t.Fatal("Expected CurrentOrg to be populated, got nil")
	}

	if renderCtx.CurrentOrg.ID != server.IDHasher.Encrypt(int(org.ID)) {
		t.Errorf("Expected org ID %s, got %s", server.IDHasher.Encrypt(int(org.ID)), renderCtx.CurrentOrg.ID)
	}

	if renderCtx.Property != nil {
		t.Error("Expected Property to be nil for org rule")
	}

	if len(renderCtx.Token) == 0 {
		t.Error("Expected CSRF token to be populated")
	}
}

func TestGetPropertyEditRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	rule, err := createPropertyRuleUserAgent(ctx, user, prop.ID, "Test Rule")
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/rules/%s/edit",
		server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)), server.IDHasher.Encrypt(int(rule.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(prop.ID)))
	req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rule.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getPropertyEditRule(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != ruleTemplate {
		t.Errorf("Expected view to be %s, got %s", ruleTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*RuleWizardRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *RuleWizardRenderContext, got %T", viewModel.Model)
	}

	if !renderCtx.IsEdit {
		t.Error("Expected IsEdit to be true")
	}

	if renderCtx.RuleID != server.IDHasher.Encrypt(int(rule.ID)) {
		t.Errorf("Expected RuleID %s, got %s", server.IDHasher.Encrypt(int(rule.ID)), renderCtx.RuleID)
	}

	if renderCtx.Name != rule.Name {
		t.Errorf("Expected rule name %s, got %s", rule.Name, renderCtx.Name)
	}
}

func TestGetOrgEditRule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	rule, err := createOrgRuleUserAgent(ctx, user, org.ID, "Test Org Rule")
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/rules/%s/edit",
		server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(rule.ID))), nil)
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rule.ID)))

	w := httptest.NewRecorder()

	viewModel, err := server.getOrgEditRule(w, req)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if viewModel == nil {
		t.Fatal("Expected ViewModel to be populated, got nil")
	}

	if viewModel.View != ruleTemplate {
		t.Errorf("Expected view to be %s, got %s", ruleTemplate, viewModel.View)
	}

	renderCtx, ok := viewModel.Model.(*RuleWizardRenderContext)
	if !ok {
		t.Fatalf("Expected Model to be *RuleWizardRenderContext, got %T", viewModel.Model)
	}

	if !renderCtx.IsEdit {
		t.Error("Expected IsEdit to be true")
	}

	if renderCtx.RuleID != server.IDHasher.Encrypt(int(rule.ID)) {
		t.Errorf("Expected RuleID %s, got %s", server.IDHasher.Encrypt(int(rule.ID)), renderCtx.RuleID)
	}

	if renderCtx.Name != rule.Name {
		t.Errorf("Expected rule name %s, got %s", rule.Name, renderCtx.Name)
	}

	if renderCtx.Property != nil {
		t.Error("Expected Property to be nil for org rule")
	}
}

type memberRuleCreationTestSuite struct {
	createTarget      func(ctx context.Context, t *testing.T, owner *dbgen.User, org *dbgen.Organization) *dbgen.Property
	postRule          func(srv *http.ServeMux, cookie *http.Cookie, member *dbgen.User, org *dbgen.Organization, prop *dbgen.Property) *http.Response
	countRules        func(ctx context.Context, t *testing.T, org *dbgen.Organization, prop *dbgen.Property) int
	afterJoinSucceeds bool
}

func runMemberRuleCreationPortalTest(t *testing.T, suite memberRuleCreationTestSuite) {
	t.Helper()
	ctx := t.Context()

	owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
	if err != nil {
		t.Fatalf("Failed to create owner account: %v", err)
	}

	prop := suite.createTarget(ctx, t, owner, org)

	member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
	if err != nil {
		t.Fatalf("Failed to create member account: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: user who is not yet a member cannot create rule
	resp := suite.postRule(srv, cookie, member, org, prop)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected redirect for non-member, got %v", resp.StatusCode)
	}
	location, _ := resp.Location()
	if location == nil || !strings.Contains(location.String(), "error") {
		t.Fatalf("Expected redirect to error page for non-member, got: %v", location)
	}
	if count := suite.countRules(ctx, t, org, prop); count != 0 {
		t.Errorf("Expected 0 rules after non-member attempt, got %d", count)
	}

	// Step 2: invite member to org
	if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
		t.Fatalf("Failed to invite member to org: %v", err)
	}

	// Step 3: invited (but not joined) member cannot create rule
	resp = suite.postRule(srv, cookie, member, org, prop)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected redirect for invited-but-not-joined member, got %v", resp.StatusCode)
	}
	location, _ = resp.Location()
	if location == nil || !strings.Contains(location.String(), "error") {
		t.Fatalf("Expected redirect to error page for invited member, got: %v", location)
	}
	if count := suite.countRules(ctx, t, org, prop); count != 0 {
		t.Errorf("Expected 0 rules after invited-but-not-joined member attempt, got %d", count)
	}

	// Step 4: member joins the org
	if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
		t.Fatalf("Failed for member to join org: %v", err)
	}

	// Step 5: verify behavior after joining depending on whether the suite expects success
	resp = suite.postRule(srv, cookie, member, org, prop)
	if suite.afterJoinSucceeds {
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("Expected redirect (success) after joining, got %v", resp.StatusCode)
		}
		location, _ = resp.Location()
		if location == nil || strings.Contains(location.String(), "error") {
			t.Fatalf("Expected successful redirect after joining, got: %v", location)
		}
		if count := suite.countRules(ctx, t, org, prop); count != 1 {
			t.Errorf("Expected 1 rule after member joined, got %d", count)
		}
	} else {
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("Expected redirect for non-owner joined member, got %v", resp.StatusCode)
		}
		location, _ = resp.Location()
		if location == nil || !strings.Contains(location.String(), "error") {
			t.Fatalf("Expected redirect to error page for non-owner member, got: %v", location)
		}
		if count := suite.countRules(ctx, t, org, prop); count != 0 {
			t.Errorf("Expected 0 rules after non-owner joined member attempt, got %d", count)
		}
	}
}

func TestMemberRuleCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("PropertyRule", func(t *testing.T) {
		runMemberRuleCreationPortalTest(t, memberRuleCreationTestSuite{
			createTarget: func(ctx context.Context, t *testing.T, owner *dbgen.User, org *dbgen.Organization) *dbgen.Property {
				safeName := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
				prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, safeName+".example.com"), org)
				if err != nil {
					t.Fatalf("Failed to create property: %v", err)
				}
				return prop
			},
			postRule: func(srv *http.ServeMux, cookie *http.Cookie, member *dbgen.User, org *dbgen.Organization, prop *dbgen.Property) *http.Response {
				return postCreatePropertyRule(srv, cookie, member, org, prop, "Test Rule", "curl", "50")
			},
			countRules: func(ctx context.Context, t *testing.T, org *dbgen.Organization, prop *dbgen.Property) int {
				rulesMap, err := store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, map[int32]uint{prop.ID: 0})
				if err != nil {
					t.Fatalf("Failed to retrieve property rules: %v", err)
				}
				return len(rulesMap[prop.ID])
			},
			afterJoinSucceeds: true,
		})
	})

	t.Run("OrgRule", func(t *testing.T) {
		runMemberRuleCreationPortalTest(t, memberRuleCreationTestSuite{
			createTarget: func(ctx context.Context, t *testing.T, owner *dbgen.User, org *dbgen.Organization) *dbgen.Property {
				return nil // org-level rules do not need a property
			},
			postRule: func(srv *http.ServeMux, cookie *http.Cookie, member *dbgen.User, org *dbgen.Organization, prop *dbgen.Property) *http.Response {
				return postCreateOrgRule(srv, cookie, member, org, "Test Org Rule",
					string(dbgen.RuleConditionPropertyUserAgent),
					string(dbgen.RuleConditionOperatorContains),
					"curl",
					string(dbgen.RuleActionPropertyDifficultyLevelPercent),
					"50")
			},
			countRules: func(ctx context.Context, t *testing.T, org *dbgen.Organization, prop *dbgen.Property) int {
				rulesMap, err := store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{org.ID: 0})
				if err != nil {
					t.Fatalf("Failed to retrieve org rules: %v", err)
				}
				return len(rulesMap[org.ID])
			},
			afterJoinSucceeds: false,
		})
	})
}

func TestDifficultyRuleToDisplayAllSwitchCases(t *testing.T) {
	t.Parallel()

	hasher := common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "salt"))

	tests := []struct {
		name                   string
		rule                   *dbgen.DifficultyRule
		expectedActionAction   string
		expectedActionProperty string
		expectedActionValue    string
		expectedCondOperator   string
	}{
		{
			name: "nil rule returns empty model",
		},
		{
			name: "action HTTPRequest shows block",
			rule: &dbgen.DifficultyRule{
				ID:                1,
				Name:              "Block rule",
				ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator: dbgen.RuleConditionOperatorEquals,
				ActionProperty:    dbgen.RuleActionPropertyHTTPRequest,
				ActionValue:       0,
			},
			expectedActionAction:   "block",
			expectedActionProperty: "HTTP request",
			expectedActionValue:    "",
			expectedCondOperator:   "equals",
		},
		{
			name: "action DifficultyLevelPercent positive",
			rule: &dbgen.DifficultyRule{
				ID:                2,
				Name:              "Increase difficulty",
				ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator: dbgen.RuleConditionOperatorContains,
				ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:       50,
			},
			expectedActionAction:   "change",
			expectedActionProperty: "Difficulty level",
			expectedActionValue:    "+50%",
			expectedCondOperator:   "contains",
		},
		{
			name: "action DifficultyLevelPercent negative",
			rule: &dbgen.DifficultyRule{
				ID:                3,
				Name:              "Decrease difficulty",
				ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
				ConditionOperator: dbgen.RuleConditionOperatorMatches,
				ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:       -30,
			},
			expectedActionAction:   "change",
			expectedActionProperty: "Difficulty level",
			expectedActionValue:    "-30%",
			expectedCondOperator:   "matches",
		},
		{
			name: "action DifficultyGrowth",
			rule: &dbgen.DifficultyRule{
				ID:                4,
				Name:              "Set growth",
				ConditionProperty: dbgen.RuleConditionPropertyCountryCode,
				ConditionOperator: dbgen.RuleConditionOperatorIn,
				ActionProperty:    dbgen.RuleActionPropertyDifficultyGrowth,
				ActionValue:       2,
			},
			expectedActionAction:   "set",
			expectedActionProperty: "Difficulty growth",
			expectedActionValue:    string(growthLevelFromIndex(2)),
			expectedCondOperator:   "is one of",
		},
		{
			name: "action Break",
			rule: &dbgen.DifficultyRule{
				ID:                5,
				Name:              "Break",
				ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator: dbgen.RuleConditionOperatorBot,
				ActionProperty:    dbgen.RuleActionPropertyBreak,
			},
			expectedActionAction:   "stop",
			expectedActionProperty: "processing rules",
			expectedActionValue:    "",
			expectedCondOperator:   "is known bot",
		},
		{
			name: "action default unknown action property",
			rule: &dbgen.DifficultyRule{
				ID:                6,
				Name:              "Unknown action",
				ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator: dbgen.RuleConditionOperatorEmpty,
				ActionProperty:    "some_unknown_action",
				ActionValue:       42,
			},
			expectedActionAction:   "set",
			expectedActionProperty: "Some Unknown Action",
			expectedActionValue:    "42",
			expectedCondOperator:   "is empty",
		},
		{
			name: "condition operator Empty negated",
			rule: &dbgen.DifficultyRule{
				ID:                       7,
				Name:                     "Not empty",
				ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator:        dbgen.RuleConditionOperatorEmpty,
				ConditionOperatorNegated: true,
				ActionProperty:           dbgen.RuleActionPropertyHTTPRequest,
			},
			expectedActionAction:   "block",
			expectedActionProperty: "HTTP request",
			expectedCondOperator:   "is not empty",
		},
		{
			name: "condition operator In negated",
			rule: &dbgen.DifficultyRule{
				ID:                       8,
				Name:                     "Not in",
				ConditionProperty:        dbgen.RuleConditionPropertyCountryCode,
				ConditionOperator:        dbgen.RuleConditionOperatorIn,
				ConditionOperatorNegated: true,
				ActionProperty:           dbgen.RuleActionPropertyHTTPRequest,
			},
			expectedActionAction:   "block",
			expectedActionProperty: "HTTP request",
			expectedCondOperator:   "is not one of",
		},
		{
			name: "condition operator Bot negated",
			rule: &dbgen.DifficultyRule{
				ID:                       9,
				Name:                     "Not bot",
				ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator:        dbgen.RuleConditionOperatorBot,
				ConditionOperatorNegated: true,
				ActionProperty:           dbgen.RuleActionPropertyHTTPRequest,
			},
			expectedActionAction:   "block",
			expectedActionProperty: "HTTP request",
			expectedCondOperator:   "is not known bot",
		},
		{
			name: "condition operator default negated",
			rule: &dbgen.DifficultyRule{
				ID:                       10,
				Name:                     "Default negated",
				ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator:        dbgen.RuleConditionOperatorContains,
				ConditionOperatorNegated: true,
				ActionProperty:           dbgen.RuleActionPropertyHTTPRequest,
			},
			expectedActionAction:   "block",
			expectedActionProperty: "HTTP request",
			expectedCondOperator:   "not contains",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := difficultyRuleToDisplay(tt.rule, true, hasher, nil)
			if model == nil {
				t.Fatal("Expected non-nil model")
			}
			if tt.rule == nil {
				if !model.CanEdit {
					t.Error("Expected CanEdit to be true for nil rule")
				}
				return
			}
			if model.ActionAction != tt.expectedActionAction {
				t.Errorf("ActionAction = %q, want %q", model.ActionAction, tt.expectedActionAction)
			}
			if model.ActionProperty != tt.expectedActionProperty {
				t.Errorf("ActionProperty = %q, want %q", model.ActionProperty, tt.expectedActionProperty)
			}
			if model.ActionValue != tt.expectedActionValue {
				t.Errorf("ActionValue = %q, want %q", model.ActionValue, tt.expectedActionValue)
			}
			if model.ConditionOperator != tt.expectedCondOperator {
				t.Errorf("ConditionOperator = %q, want %q", model.ConditionOperator, tt.expectedCondOperator)
			}
		})
	}
}

func TestRegisterAction(t *testing.T) {
	t.Parallel()

	registry := &RuleRegistry{
		conditions: map[string]ConditionRegistration{},
		actions:    map[string]ActionFormParser{},
	}

	t.Run("empty key returns error", func(t *testing.T) {
		err := registry.RegisterAction("", func(v string) (int32, common.StatusCode) {
			return 0, common.StatusOK
		})
		if err == nil {
			t.Fatal("Expected error for empty key")
		}
		if err != errRuleActionEmpty {
			t.Errorf("Expected errRuleActionEmpty, got: %v", err)
		}
	})

	t.Run("valid registration", func(t *testing.T) {
		err := registry.RegisterAction("test_action", func(v string) (int32, common.StatusCode) {
			return 42, common.StatusOK
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		parser, ok := registry.ActionParser("test_action")
		if !ok {
			t.Fatal("Expected action parser to be registered")
		}
		val, status := parser("anything")
		if val != 42 || status != common.StatusOK {
			t.Errorf("Unexpected parser result: val=%d, status=%v", val, status)
		}
	})

	t.Run("unregistered key returns false", func(t *testing.T) {
		_, ok := registry.ActionParser("nonexistent")
		if ok {
			t.Error("Expected false for unregistered key")
		}
	})
}

func TestPostPropertyNewRuleParseRuleFormFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(org.UserID.Int32, t.Name()+".example.com"), org)
	if err != nil {
		t.Fatalf("Failed to create property: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Send form with empty name to trigger parseRuleForm failure
	resp := postCreatePropertyRule(srv, cookie, user, org, prop, "", "curl", "50")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 (form re-rendered with error), got %v", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), common.StatusRuleNameEmptyError.String()) {
		t.Error("Expected error message about empty rule name in response body")
	}
}

func TestPostOrgNewRuleParseRuleFormFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	org, _, err := store.Impl().CreateNewOrganization(ctx, t.Name()+"-org", user.ID)
	if err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Send form with empty name to trigger parseRuleForm failure
	resp := postCreateOrgRule(srv, cookie, user, org, "",
		string(dbgen.RuleConditionPropertyUserAgent),
		string(dbgen.RuleConditionOperatorContains),
		"curl",
		string(dbgen.RuleActionPropertyDifficultyLevelPercent),
		"50")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 (form re-rendered with error), got %v", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), common.StatusRuleNameEmptyError.String()) {
		t.Error("Expected error message about empty rule name in response body")
	}
}

func TestIsRuleNameValidInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"contains exclamation", "Hello!"},
		{"contains at sign", "rule@name"},
		{"contains slash", "rule/name"},
		{"contains hash", "rule#1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if isRuleNameValid(tt.input) {
				t.Errorf("Expected isRuleNameValid(%q) to return false", tt.input)
			}
		})
	}
}

func TestParseRuleFormNegativeCases(t *testing.T) {
	t.Parallel()

	registry := NewRuleRegistry()

	makeServer := func() *Server {
		return &Server{
			Rules: registry,
		}
	}

	tests := []struct {
		name         string
		formValues   map[string]string
		expectedCode common.StatusCode
	}{
		{
			name:         "empty name",
			formValues:   map[string]string{},
			expectedCode: common.StatusRuleNameEmptyError,
		},
		{
			name: "invalid name chars",
			formValues: map[string]string{
				common.ParamName: "Rule@invalid!",
			},
			expectedCode: common.StatusRuleNameInvalidCharsError,
		},
		{
			name: "empty condition property",
			formValues: map[string]string{
				common.ParamName: "Valid Name",
			},
			expectedCode: common.StatusRuleConditionPropertyRequired,
		},
		{
			name: "empty action property",
			formValues: map[string]string{
				common.ParamName:              "Valid Name",
				common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
			},
			expectedCode: common.StatusRuleActionPropertyRequired,
		},
		{
			name: "invalid condition property",
			formValues: map[string]string{
				common.ParamName:              "Valid Name",
				common.ParamConditionProperty: "unknown_condition",
				common.ParamActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
			},
			expectedCode: common.StatusRuleConditionPropertyInvalid,
		},
		{
			name: "invalid action property",
			formValues: map[string]string{
				common.ParamName:              "Valid Name",
				common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
				common.ParamActionProperty:    "unknown_action",
			},
			expectedCode: common.StatusRuleActionPropertyInvalid,
		},
		{
			name: "condition parser fails",
			formValues: map[string]string{
				common.ParamName:              "Valid Name",
				common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
				common.ParamConditionOperator: "invalid_operator",
				common.ParamActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
				common.ParamActionValue:       "50",
			},
			expectedCode: common.StatusRuleConditionOperatorInvalid,
		},
		{
			name: "action parser fails",
			formValues: map[string]string{
				common.ParamName:              "Valid Name",
				common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
				common.ParamConditionOperator: string(dbgen.RuleConditionOperatorBot),
				common.ParamActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
				common.ParamActionValue:       "0",
			},
			expectedCode: common.StatusRuleDifficultyValueInvalid,
		},
		{
			name: "negated operator suffix is trimmed",
			formValues: map[string]string{
				common.ParamName:              "Valid Name",
				common.ParamConditionProperty: string(dbgen.RuleConditionPropertyUserAgent),
				common.ParamConditionOperator: string(dbgen.RuleConditionOperatorContains) + "_negated",
				common.ParamConditionValue:    "test",
				common.ParamActionProperty:    string(dbgen.RuleActionPropertyDifficultyLevelPercent),
				common.ParamActionValue:       "50",
			},
			expectedCode: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := makeServer()
			form := url.Values{}
			for k, v := range tt.formValues {
				form.Set(k, v)
			}

			req := httptest.NewRequest("POST", "/test", strings.NewReader(form.Encode()))
			req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
			if err := req.ParseForm(); err != nil {
				t.Fatalf("Failed to parse form: %v", err)
			}

			renderCtx := &RuleWizardRenderContext{}
			_, statusCode := s.parseRuleForm(t.Context(), req, renderCtx, "example.com")

			if statusCode != tt.expectedCode {
				t.Errorf("parseRuleForm() = %v, want %v", statusCode, tt.expectedCode)
			}
		})
	}
}

type cannotEditRuleTestSuite struct {
	name string
	run  func(t *testing.T, owner *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, rule *dbgen.DifficultyRule, member *dbgen.User, cookie *http.Cookie, srv *http.ServeMux)
}

func TestMemberCannotEditRuleSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	suites := []cannotEditRuleTestSuite{
		{
			name: "getPropertyEditRule",
			run: func(t *testing.T, owner *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, rule *dbgen.DifficultyRule, member *dbgen.User, cookie *http.Cookie, srv *http.ServeMux) {
				req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/property/%s/rules/%s/edit",
					server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)), server.IDHasher.Encrypt(int(rule.ID))), nil)
				req.AddCookie(cookie)
				req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
				req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(prop.ID)))
				req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rule.ID)))

				w := httptest.NewRecorder()
				_, err := server.getPropertyEditRule(w, req)
				if err == nil {
					t.Fatal("Expected error from getPropertyEditRule")
				}
			},
		},
		{
			name: "postPropertyEditRule",
			run: func(t *testing.T, owner *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, rule *dbgen.DifficultyRule, member *dbgen.User, cookie *http.Cookie, srv *http.ServeMux) {
				resp := postEditPropertyRule(srv, cookie, member, org, prop, rule, "Updated",
					string(dbgen.RuleConditionOperatorEquals), "test", "50")
				if resp.StatusCode != http.StatusSeeOther {
					t.Fatalf("Expected redirect, got %v", resp.StatusCode)
				}
				location, _ := resp.Location()
				if location == nil || !strings.Contains(location.String(), "error") {
					t.Errorf("Expected redirect to error page, got: %v", location)
				}
			},
		},
		{
			name: "getOrgEditRule",
			run: func(t *testing.T, owner *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, rule *dbgen.DifficultyRule, member *dbgen.User, cookie *http.Cookie, srv *http.ServeMux) {
				req := httptest.NewRequest("GET", fmt.Sprintf("/org/%s/rules/%s/edit",
					server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(rule.ID))), nil)
				req.AddCookie(cookie)
				req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
				req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rule.ID)))

				w := httptest.NewRecorder()
				_, err := server.getOrgEditRule(w, req)
				if err == nil {
					t.Fatal("Expected error from getOrgEditRule")
				}
			},
		},
		{
			name: "postOrgEditRule",
			run: func(t *testing.T, owner *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, rule *dbgen.DifficultyRule, member *dbgen.User, cookie *http.Cookie, srv *http.ServeMux) {
				resp := postEditOrgRule(srv, cookie, member, org, rule, "Updated",
					string(dbgen.RuleConditionPropertyUserAgent),
					string(dbgen.RuleConditionOperatorEquals),
					"test",
					string(dbgen.RuleActionPropertyDifficultyLevelPercent),
					"50")
				if resp.StatusCode != http.StatusSeeOther {
					t.Fatalf("Expected redirect, got %v", resp.StatusCode)
				}
				location, _ := resp.Location()
				if location == nil || !strings.Contains(location.String(), "error") {
					t.Errorf("Expected redirect to error page, got: %v", location)
				}
			},
		},
		{
			name: "postMovePropertyRule",
			run: func(t *testing.T, owner *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, rule *dbgen.DifficultyRule, member *dbgen.User, cookie *http.Cookie, srv *http.ServeMux) {
				form := url.Values{}
				form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(member.ID))))
				form.Set(common.ParamPosition, "0")

				req := httptest.NewRequest("POST", "/endpoint", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(cookie)
				req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
				req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(prop.ID)))
				req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rule.ID)))

				w := httptest.NewRecorder()
				_, err := server.postMovePropertyRule(w, req)
				if err == nil {
					t.Fatal("Expected error from postMovePropertyRule")
				}
			},
		},
		{
			name: "postMoveOrgRule",
			run: func(t *testing.T, owner *dbgen.User, org *dbgen.Organization, prop *dbgen.Property, rule *dbgen.DifficultyRule, member *dbgen.User, cookie *http.Cookie, srv *http.ServeMux) {
				form := url.Values{}
				form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(member.ID))))
				form.Set(common.ParamPosition, "0")

				req := httptest.NewRequest("POST", "/endpoint", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(cookie)
				req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
				req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rule.ID)))

				w := httptest.NewRecorder()
				_, err := server.postMoveOrgRule(w, req)
				if err == nil {
					t.Fatal("Expected error from postMoveOrgRule")
				}
			},
		},
	}

	for _, suite := range suites {
		t.Run(suite.name, func(t *testing.T) {
			ctx := t.Context()

			owner, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_owner", testPlan)
			if err != nil {
				t.Fatalf("Failed to create owner account: %v", err)
			}

			safeName := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
			prop, _, err := store.Impl().CreateNewProperty(ctx, db_tests.CreateNewPropertyParams(owner.ID, safeName+".example.com"), org)
			if err != nil {
				t.Fatalf("Failed to create property: %v", err)
			}

			// Owner creates a rule
			rule, _, err := store.Impl().CreateDifficultyRule(ctx, owner, &dbgen.CreateDifficultyRuleParams{
				Name:              "Owner Rule",
				PropertyID:        db.Int(prop.ID),
				OrgID:             db.Int(org.ID),
				Enabled:           true,
				ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
				ConditionOperator: dbgen.RuleConditionOperatorContains,
				ConditionValueStr: db.Text("test"),
				ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
				ActionValue:       50,
				CreatorID:         db.Int(owner.ID),
			})
			if err != nil {
				t.Fatalf("Failed to create rule: %v", err)
			}

			// Create member and join org
			member, _, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name()+"_member", testPlan)
			if err != nil {
				t.Fatalf("Failed to create member account: %v", err)
			}

			if _, err := store.Impl().InviteUserToOrg(ctx, owner, org, member); err != nil {
				t.Fatalf("Failed to invite member: %v", err)
			}
			if _, err := store.Impl().JoinOrg(ctx, org.ID, member); err != nil {
				t.Fatalf("Failed to join org: %v", err)
			}

			srv := http.NewServeMux()
			server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

			cookie, err := portal_tests.AuthenticateSuite(ctx, member.Email, srv, server.XSRF, server.Sessions)
			if err != nil {
				t.Fatal(err)
			}

			suite.run(t, owner, org, prop, rule, member, cookie, srv)
		})
	}
}
