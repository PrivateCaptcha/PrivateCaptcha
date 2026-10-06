package common

import "testing"

func TestEnabledFeatureFlags(t *testing.T) {
	flags := EnabledFeatureFlags{}
	userID := int32(42)
	orgID := int32(123)
	if !flags.Enabled(t.Context(), FeatureArgon2ID, &userID, &orgID) {
		t.Fatal("core feature flags should be enabled")
	}
	if !flags.Enabled(t.Context(), "unknown", nil, nil) {
		t.Fatal("core feature flags should be enabled without subjects")
	}
}
