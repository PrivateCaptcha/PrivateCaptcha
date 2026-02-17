package portal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	db_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/tests"
	portal_tests "github.com/PrivateCaptcha/PrivateCaptcha/pkg/portal/tests"
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

func createPropertyRuleForMove(ctx context.Context, user *dbgen.User, propertyID int32, name string, actionValue int32) (*dbgen.DifficultyRule, error) {
	rule, _, err := server.Store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:                     name,
		PropertyID:               db.Int(propertyID),
		Enabled:                  true,
		ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator:        dbgen.RuleConditionOperatorContains,
		ConditionOperatorNegated: false,
		ConditionValueStr:        db.Text("test"),
		ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:              actionValue,
		CreatorID:                db.Int(user.ID),
		Column14:                 100.0,
	})
	return rule, err
}

func createOrgRuleForMove(ctx context.Context, user *dbgen.User, orgID int32, name string, actionValue int32) (*dbgen.DifficultyRule, error) {
	rule, _, err := server.Store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:                     name,
		OrgID:                    db.Int(orgID),
		Enabled:                  true,
		ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator:        dbgen.RuleConditionOperatorContains,
		ConditionOperatorNegated: false,
		ConditionValueStr:        db.Text("test"),
		ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:              actionValue,
		CreatorID:                db.Int(user.ID),
		Column14:                 100.0,
	})
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
			name:     "equals with value is valid",
			operator: string(dbgen.RuleConditionOperatorEquals),
			value:    "test",
			expected: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &RuleWizardRenderContext{}
			ctx.ConditionOperator = tt.operator
			ctx.ConditionValue = tt.value
			if got := ctx.parseUserAgentCondition(); got != tt.expected {
				t.Errorf("parseUserAgentCondition() = %v, want %v", got, tt.expected)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &RuleWizardRenderContext{}
			ctx.ConditionOperator = tt.operator
			ctx.ConditionValue = tt.value
			if got := ctx.parseIPAddressCondition(); got != tt.expected {
				t.Errorf("parseIPAddressCondition() = %v, want %v", got, tt.expected)
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
			ctx := &RuleWizardRenderContext{}
			ctx.ConditionOperator = tt.operator
			ctx.ConditionValue = tt.value
			if got := ctx.parseCountryCodeCondition(); got != tt.expected {
				t.Errorf("parseCountryCodeCondition() = %v, want %v", got, tt.expected)
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
			value:        "-101",
			expectedVal:  0,
			expectedCode: common.StatusRuleDifficultyValueInvalid,
		},
		{
			name:         "value too high",
			value:        "101",
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
			value:        "-100",
			expectedVal:  -100,
			expectedCode: common.StatusOK,
		},
		{
			name:         "boundary max value",
			value:        "100",
			expectedVal:  100,
			expectedCode: common.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &RuleWizardRenderContext{}
			ctx.ActionValue = tt.value
			val, code := ctx.parseDifficultyAction()
			if code != tt.expectedCode {
				t.Errorf("parseDifficultyAction() code = %v, want %v", code, tt.expectedCode)
			}
			if val != tt.expectedVal {
				t.Errorf("parseDifficultyAction() val = %v, want %v", val, tt.expectedVal)
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
			ctx := &RuleWizardRenderContext{}
			ctx.ActionValue = tt.value
			val, code := ctx.parseDifficultyGrowthAction()
			if code != tt.expectedCode {
				t.Errorf("parseDifficultyGrowthAction() code = %v, want %v", code, tt.expectedCode)
			}
			if val != tt.expectedVal {
				t.Errorf("parseDifficultyGrowthAction() val = %v, want %v", val, tt.expectedVal)
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
			ctx := &RuleWizardRenderContext{}
			ctx.ActionValue = tt.value
			val, code := ctx.parseHTTPRequestAction()
			if code != tt.expectedCode {
				t.Errorf("parseHTTPRequestAction() code = %v, want %v", code, tt.expectedCode)
			}
			if val != tt.expectedVal {
				t.Errorf("parseHTTPRequestAction() val = %v, want %v", val, tt.expectedVal)
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

			// Read rules via server's getPropertyRules method
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

			// Read rules via server's createOrgRulesContext method
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

func TestMovePropertyRuleSingleRule(t *testing.T) {
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

	// Create single rule
	rule, err := createPropertyRuleForMove(ctx, user, property.ID, "Test Rule", 10)
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Try to move the single rule (should succeed even though it's the only rule)
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Add(common.ParamPosition, "0")

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/%s/rules/%s/move",
		server.IDHasher.Encrypt(int(org.ID)),
		server.IDHasher.Encrypt(int(property.ID)),
		server.IDHasher.Encrypt(int(rule.ID))), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rule.ID)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 See Other, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMovePropertyRuleToFirstPosition(t *testing.T) {
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
	rules := make([]*dbgen.DifficultyRule, 3)
	for i := 0; i < 3; i++ {
		rule, err := createPropertyRuleForMove(ctx, user, property.ID, fmt.Sprintf("Test Rule %d", i), int32(10+i))
		if err != nil {
			t.Fatalf("Failed to create rule %d: %v", i, err)
		}
		rules[i] = rule
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Move last rule to first position
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Add(common.ParamPosition, "0")

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
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 See Other, got %d: %s", w.Code, w.Body.String())
	}

	// Verify rule is now first
	allRules, err := server.Store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, map[int32]uint{property.ID: 0})
	if err != nil {
		t.Fatalf("Failed to retrieve rules: %v", err)
	}
	propertyRules := allRules[property.ID]
	if len(propertyRules) != 3 {
		t.Fatalf("Expected 3 rules, got %d", len(propertyRules))
	}
	if propertyRules[0].ID != rules[2].ID {
		t.Errorf("Expected first rule to be rule 2, got rule %d", propertyRules[0].ID)
	}
}

func TestMovePropertyRuleToLastPosition(t *testing.T) {
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
	rules := make([]*dbgen.DifficultyRule, 3)
	for i := 0; i < 3; i++ {
		rule, err := createPropertyRuleForMove(ctx, user, property.ID, fmt.Sprintf("Test Rule %d", i), int32(10+i))
		if err != nil {
			t.Fatalf("Failed to create rule %d: %v", i, err)
		}
		rules[i] = rule
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Move first rule to last position (index 2)
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Add(common.ParamPosition, "2")

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/property/%s/rules/%s/move",
		server.IDHasher.Encrypt(int(org.ID)),
		server.IDHasher.Encrypt(int(property.ID)),
		server.IDHasher.Encrypt(int(rules[0].ID))), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamProperty, server.IDHasher.Encrypt(int(property.ID)))
	req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rules[0].ID)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 See Other, got %d: %s", w.Code, w.Body.String())
	}

	// Verify rule is now last
	allRules, err := server.Store.Impl().RetrieveDifficultyRulesByPropertyIDs(ctx, map[int32]uint{property.ID: 0})
	if err != nil {
		t.Fatalf("Failed to retrieve rules: %v", err)
	}
	propertyRules := allRules[property.ID]
	if len(propertyRules) != 3 {
		t.Fatalf("Expected 3 rules, got %d", len(propertyRules))
	}
	if propertyRules[2].ID != rules[0].ID {
		t.Errorf("Expected last rule to be rule 0, got rule %d", propertyRules[2].ID)
	}
}

func TestMoveOrgRuleMultipleRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	user, org, err := db_tests.CreateNewAccountForTest(ctx, store, t.Name(), testPlan)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Create multiple org-level rules
	rules := make([]*dbgen.DifficultyRule, 3)
	for i := 0; i < 3; i++ {
		rule, err := createOrgRuleForMove(ctx, user, org.ID, fmt.Sprintf("Test Org Rule %d", i), int32(10+i))
		if err != nil {
			t.Fatalf("Failed to create rule %d: %v", i, err)
		}
		rules[i] = rule
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	// Move middle rule to first position
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Add(common.ParamPosition, "0")

	req := httptest.NewRequest("POST", fmt.Sprintf("/org/%s/rules/%s/move",
		server.IDHasher.Encrypt(int(org.ID)),
		server.IDHasher.Encrypt(int(rules[1].ID))), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
	req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(rules[1].ID)))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 See Other, got %d: %s", w.Code, w.Body.String())
	}

	// Verify rule is now first
	allRules, err := server.Store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{org.ID: 0})
	if err != nil {
		t.Fatalf("Failed to retrieve rules: %v", err)
	}
	orgRules := allRules[org.ID]
	if len(orgRules) != 3 {
		t.Fatalf("Expected 3 rules, got %d", len(orgRules))
	}
	if orgRules[0].ID != rules[1].ID {
		t.Errorf("Expected first rule to be rule 1, got rule %d", orgRules[0].ID)
	}
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
		rule, err := createPropertyRuleForMove(ctx, user, property.ID, fmt.Sprintf("Test Rule %d", i), int32(10+i))
		if err != nil {
			t.Fatalf("Failed to create rule %d: %v", i, err)
		}
		rules[i] = rule
	}

	// Corrupt positions to force rebalancing
	if err := db_tests.CorruptDifficultyRulePositionsForTest(ctx, store, &property.ID, nil); err != nil {
		t.Fatalf("Failed to corrupt positions: %v", err)
	}

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
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 See Other, got %d: %s", w.Code, w.Body.String())
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
		if delta < 50.0 {
			t.Errorf("Rules %d and %d are too close: delta = %f", i-1, i, delta)
		}
	}

	// Verify the moved rule is at position 1
	if propertyRules[1].ID != rules[2].ID {
		t.Errorf("Expected rule at index 1 to be rule 2, got rule %d", propertyRules[1].ID)
	}
}
