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
		Column14:                 db.RulePositionStep,
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
			if got := ctx.parseCountryCodeCondition(","); got != tt.expected {
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

func TestCircularMoveOrgRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

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

			// Move last rule to first position N times (where N = numRules)
			// After N moves, the order should be back to the original
			for moveNum := 0; moveNum < numRules; moveNum++ {
				// Get current rules order
				currentRules, err := server.Store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{org.ID: 0})
				if err != nil {
					t.Fatal(err)
				}
				if len(currentRules[org.ID]) != numRules {
					t.Fatalf("Expected %d rules, got %d", numRules, len(currentRules[org.ID]))
				}

				// Find the last rule's ID
				lastRuleID := currentRules[org.ID][numRules-1].ID

				// Move last rule to first position
				form := url.Values{}
				form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
				form.Add(common.ParamPosition, "0")

				req := httptest.NewRequest("POST", "/endpoint", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(cookie)
				req.SetPathValue(common.ParamOrg, server.IDHasher.Encrypt(int(org.ID)))
				req.SetPathValue(common.ParamRule, server.IDHasher.Encrypt(int(lastRuleID)))

				w := httptest.NewRecorder()
				if _, err := server.postMoveOrgRule(w, req); err != nil {
					t.Fatalf("Move %d failed: %v", moveNum+1, err)
				}

				t.Logf("Move %d: Moved rule %d to position 0", moveNum+1, lastRuleID)
			}

			// Verify final order matches original order
			finalRules, err := server.Store.Impl().RetrieveDifficultyRulesByOrgIDs(ctx, map[int32]uint{org.ID: 0})
			if err != nil {
				t.Fatal(err)
			}
			if len(finalRules[org.ID]) != numRules {
				t.Fatalf("Expected %d rules, got %d", numRules, len(finalRules[org.ID]))
			}

			for i := 0; i < numRules; i++ {
				if finalRules[org.ID][i].ID != originalRuleIDs[i] {
					t.Errorf("After %d circular moves, rule at index %d should be %d but got %d",
						numRules, i, originalRuleIDs[i], finalRules[org.ID][i].ID)
				}
			}

			t.Logf("Successfully verified circular moves for %d rules", numRules)
		})
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
			ctx := &RuleWizardRenderContext{}
			ctx.ConditionOperator = tt.operator
			ctx.ConditionValue = tt.value
			if got := ctx.parseDomainCondition(tt.domain); got != tt.expected {
				t.Errorf("parseDomainCondition() = %v, want %v", got, tt.expected)
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
