//go:build enterprise

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
	rule, _, err := server.Store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
		Name:                     "Test Rule",
		PropertyID:               db.Int(property.ID),
		Enabled:                  true,
		ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
		ConditionOperator:        dbgen.RuleConditionOperatorContains,
		ConditionOperatorNegated: false,
		ConditionValueStr:        db.Text("test"),
		ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
		ActionValue:              10,
		CreatorID:                db.Int(user.ID),
		Column14:                 100.0,
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
		rule, _, err := server.Store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
			Name:                     fmt.Sprintf("Test Rule %d", i),
			PropertyID:               db.Int(property.ID),
			Enabled:                  true,
			ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator:        dbgen.RuleConditionOperatorContains,
			ConditionOperatorNegated: false,
			ConditionValueStr:        db.Text("test"),
			ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:              int32(10 + i),
			CreatorID:                db.Int(user.ID),
			Column14:                 100.0,
		})
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
		rule, _, err := server.Store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
			Name:                     fmt.Sprintf("Test Rule %d", i),
			PropertyID:               db.Int(property.ID),
			Enabled:                  true,
			ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator:        dbgen.RuleConditionOperatorContains,
			ConditionOperatorNegated: false,
			ConditionValueStr:        db.Text("test"),
			ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:              int32(10 + i),
			CreatorID:                db.Int(user.ID),
			Column14:                 100.0,
		})
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
		rule, _, err := server.Store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
			Name:                     fmt.Sprintf("Test Org Rule %d", i),
			OrgID:                    db.Int(org.ID),
			Enabled:                  true,
			ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator:        dbgen.RuleConditionOperatorContains,
			ConditionOperatorNegated: false,
			ConditionValueStr:        db.Text("test"),
			ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:              int32(10 + i),
			CreatorID:                db.Int(user.ID),
			Column14:                 100.0,
		})
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
		rule, _, err := server.Store.Impl().CreateDifficultyRule(ctx, user, &dbgen.CreateDifficultyRuleParams{
			Name:                     fmt.Sprintf("Test Rule %d", i),
			PropertyID:               db.Int(property.ID),
			Enabled:                  true,
			ConditionProperty:        dbgen.RuleConditionPropertyUserAgent,
			ConditionOperator:        dbgen.RuleConditionOperatorContains,
			ConditionOperatorNegated: false,
			ConditionValueStr:        db.Text("test"),
			ActionProperty:           dbgen.RuleActionPropertyDifficultyLevelPercent,
			ActionValue:              int32(10 + i),
			CreatorID:                db.Int(user.ID),
			Column14:                 100.0,
		})
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
