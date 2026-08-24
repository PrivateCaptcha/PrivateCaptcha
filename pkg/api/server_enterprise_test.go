//go:build enterprise

package api

import (
	"net/http/httptest"
	"testing"
)

func TestAPIErrorHandlerIDUsesRoutePattern(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("POST", "/api/org/attacker-controlled/properties", nil)
	request.Pattern = "POST /api/org/{org}/properties"

	if got, want := apiErrorHandlerID(request), "/api/org/{org}/properties"; got != want {
		t.Fatalf("Expected handler ID %q, got %q", want, got)
	}
}

func TestAPIErrorHandlerIDWithoutPatternIsBounded(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("POST", "/api/org/attacker-controlled/properties", nil)

	if got := apiErrorHandlerID(request); got != "" {
		t.Fatalf("Expected empty handler ID, got %q", got)
	}
}
