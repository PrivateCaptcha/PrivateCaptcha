package rules

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewCompiledRulesNoRules(t *testing.T) {
	cr := NewCompiledRules(nil)
	if cr.hasBlockRequestRules {
		t.Error("expected hasBlockRequestRules to be false with no rules")
	}
}

func TestNewCompiledRulesOnlyBlockRules(t *testing.T) {
	rules := []Rule{
		NewBlockRequestRule(func(r *http.Request) bool { return true }),
	}
	cr := NewCompiledRules(rules)
	if !cr.hasBlockRequestRules {
		t.Error("expected hasBlockRequestRules to be true when block rules are present")
	}
}

func TestNewCompiledRulesNoBlockRules(t *testing.T) {
	cr := NewCompiledRules([]Rule{})
	if cr.hasBlockRequestRules {
		t.Error("expected hasBlockRequestRules to be false with no block rules")
	}
}

func TestIsRequestBlockedNoBlockRules(t *testing.T) {
	cr := NewCompiledRules(nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cr.IsRequestBlocked(req) {
		t.Error("expected IsRequestBlocked to return false when no block rules are present")
	}
}

func TestIsRequestBlockedMatchingRule(t *testing.T) {
	rules := []Rule{
		NewBlockRequestRule(func(r *http.Request) bool { return true }),
	}
	cr := NewCompiledRules(rules)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if !cr.IsRequestBlocked(req) {
		t.Error("expected IsRequestBlocked to return true when a matching block rule exists")
	}
}

func TestIsRequestBlockedNonMatchingRule(t *testing.T) {
	rules := []Rule{
		NewBlockRequestRule(func(r *http.Request) bool { return false }),
	}
	cr := NewCompiledRules(rules)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cr.IsRequestBlocked(req) {
		t.Error("expected IsRequestBlocked to return false when no block rules match")
	}
}

func TestIsRequestBlockedMultipleRulesFirstMatches(t *testing.T) {
	rules := []Rule{
		NewBlockRequestRule(func(r *http.Request) bool { return true }),
		NewBlockRequestRule(func(r *http.Request) bool { return false }),
	}
	cr := NewCompiledRules(rules)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if !cr.IsRequestBlocked(req) {
		t.Error("expected IsRequestBlocked to return true when the first rule matches")
	}
}

func TestIsRequestBlockedConditionReceivesRequest(t *testing.T) {
	const wantUserAgent = "test-agent"
	var gotUserAgent string
	rules := []Rule{
		NewBlockRequestRule(func(r *http.Request) bool {
			gotUserAgent = r.UserAgent()
			return false
		}),
	}
	cr := NewCompiledRules(rules)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", wantUserAgent)
	cr.IsRequestBlocked(req)
	if gotUserAgent != wantUserAgent {
		t.Errorf("expected condition to receive request with User-Agent %q, got %q", wantUserAgent, gotUserAgent)
	}
}
