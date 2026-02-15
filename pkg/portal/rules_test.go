package portal

import (
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

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, "Block crawlers")
	form.Set(common.ParamEnabled, "on")
	form.Set(common.ParamConditionProperty, string(dbgen.RuleConditionPropertyUserAgent))
	form.Set(common.ParamConditionOperator, string(dbgen.RuleConditionOperatorContains))
	form.Set(common.ParamConditionValue, "curl")
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

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, "Block countries")
	form.Set(common.ParamEnabled, "on")
	form.Set(common.ParamConditionProperty, string(dbgen.RuleConditionPropertyCountryCode))
	form.Set(common.ParamConditionOperator, string(dbgen.RuleConditionOperatorIn))
	form.Set(common.ParamConditionValue, "US,GB")
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

	rule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              "Original Rule",
		PropertyID:        db.Int(prop.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("curl"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, "Updated Rule")
	form.Set(common.ParamEnabled, "on")
	form.Set(common.ParamConditionProperty, string(dbgen.RuleConditionPropertyUserAgent))
	form.Set(common.ParamConditionOperator, string(dbgen.RuleConditionOperatorEquals)+"_negated")
	form.Set(common.ParamConditionValue, "bot")
	form.Set(common.ParamActionProperty, string(dbgen.RuleActionPropertyDifficultyLevelPercent))
	form.Set(common.ParamActionValue, "-30")

	ruleIDStr := server.IDHasher.Encrypt(int(rule.ID))
	req := httptest.NewRequest("POST",
		fmt.Sprintf("/org/%s/property/%s/rules/%s/edit", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)), ruleIDStr),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
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

	rule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              "Original Org Rule",
		OrgID:             db.Int(org.ID),
		Enabled:           false,
		ConditionProperty: dbgen.RuleConditionPropertyIPAddress,
		ConditionOperator: dbgen.RuleConditionOperatorMatches,
		ConditionValueStr: db.Text("10.0.0.0/8"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       20,
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	srv := http.NewServeMux()
	server.Setup(portalDomain(), common.NoopMiddleware).Register(srv)

	cookie, err := portal_tests.AuthenticateSuite(ctx, user.Email, srv, server.XSRF, server.Sessions)
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(user.ID))))
	form.Set(common.ParamName, "Updated Org Rule")
	form.Set(common.ParamEnabled, "on")
	form.Set(common.ParamConditionProperty, string(dbgen.RuleConditionPropertyCountryCode))
	form.Set(common.ParamConditionOperator, string(dbgen.RuleConditionOperatorIn)+"_negated")
	form.Set(common.ParamConditionValue, "DE,FR")
	form.Set(common.ParamActionProperty, string(dbgen.RuleActionPropertyHTTPRequest))
	form.Set(common.ParamActionValue, "block")

	ruleIDStr := server.IDHasher.Encrypt(int(rule.ID))
	req := httptest.NewRequest("POST",
		fmt.Sprintf("/org/%s/rules/%s/edit", server.IDHasher.Encrypt(int(org.ID)), ruleIDStr),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	resp := w.Result()
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
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(member.ID))))
	form.Set(common.ParamName, "Updated Rule")
	form.Set(common.ParamEnabled, "on")
	form.Set(common.ParamConditionProperty, string(dbgen.RuleConditionPropertyUserAgent))
	form.Set(common.ParamConditionOperator, string(dbgen.RuleConditionOperatorEquals))
	form.Set(common.ParamConditionValue, "bot")
	form.Set(common.ParamActionProperty, string(dbgen.RuleActionPropertyDifficultyLevelPercent))
	form.Set(common.ParamActionValue, "-30")

	req := httptest.NewRequest("POST",
		fmt.Sprintf("/org/%s/property/%s/rules/%s/edit", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(prop.ID)), server.IDHasher.Encrypt(int(rule.ID))),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

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
	form := url.Values{}
	form.Set(common.ParamCSRFToken, server.XSRF.Token(strconv.Itoa(int(member.ID))))
	form.Set(common.ParamName, "Updated Org Rule")
	form.Set(common.ParamEnabled, "on")
	form.Set(common.ParamConditionProperty, string(dbgen.RuleConditionPropertyUserAgent))
	form.Set(common.ParamConditionOperator, string(dbgen.RuleConditionOperatorEquals))
	form.Set(common.ParamConditionValue, "bot")
	form.Set(common.ParamActionProperty, string(dbgen.RuleActionPropertyDifficultyLevelPercent))
	form.Set(common.ParamActionValue, "-30")

	req := httptest.NewRequest("POST",
		fmt.Sprintf("/org/%s/rules/%s/edit", server.IDHasher.Encrypt(int(org.ID)), server.IDHasher.Encrypt(int(rule.ID))),
		strings.NewReader(form.Encode()))
	req.AddCookie(cookie)
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)

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

	rule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              "Test Rule",
		PropertyID:        db.Int(prop.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("curl"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
	})
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
	deletedRule, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}

	if !deletedRule.DeletedAt.Valid {
		t.Error("Expected rule to be soft deleted")
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

	rule, _, err := store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:              "Test Org Rule",
		OrgID:             db.Int(org.ID),
		Enabled:           true,
		ConditionProperty: dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator: dbgen.RuleConditionOperatorContains,
		ConditionValueStr: db.Text("curl"),
		ActionProperty:    dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:       50,
	})
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
	deletedRule, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}

	if !deletedRule.DeletedAt.Valid {
		t.Error("Expected rule to be soft deleted")
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
	unchangedRule, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}
	if unchangedRule.DeletedAt.Valid {
		t.Error("Rule was incorrectly deleted")
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
	unchangedRule, err := store.Impl().RetrieveDifficultyRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve rule: %v", err)
	}
	if unchangedRule.DeletedAt.Valid {
		t.Error("Rule was incorrectly deleted")
	}
}
