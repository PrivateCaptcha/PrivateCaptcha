package config

import (
	"context"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

func TestRegisterEnvName(t *testing.T) {
	if err := RegisterEnvNameForConfigKey(common.COMMON_CONFIG_KEYS_COUNT, "count"); err != nil {
		t.Fatal(err)
	}
}

func TestEnvConfigValueUpdate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		key            common.ConfigKey
		envValue       string
		expectedValue  string
		expectedError  bool
	}{
		{"valid_key_with_value", common.StageKey, "production", "production", false},
		{"valid_key_empty_value", common.StageKey, "", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(name string) string {
				return tc.envValue
			}

			v := &envConfigValue{key: tc.key}
			err := v.Update(getenv)

			if tc.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if v.Value() != tc.expectedValue {
				t.Errorf("Value mismatch: got %q, want %q", v.Value(), tc.expectedValue)
			}
		})
	}
}

func TestEnvConfigValueUpdateUnknownKey(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		return "test"
	}

	// Use a key value that's definitely out of range
	v := &envConfigValue{key: common.ConfigKey(9999)}
	err := v.Update(getenv)

	if err != errEmptyEnvName {
		t.Errorf("Expected errEmptyEnvName for unknown key, got: %v", err)
	}
}

func TestEnvConfigUpdate(t *testing.T) {
	t.Parallel()

	initialValue := "initial"
	updatedValue := "updated"
	currentValue := initialValue

	getenv := func(name string) string {
		return currentValue
	}

	cfg := NewEnvConfig(getenv)

	// Access a key to populate the items map
	item := cfg.Get(common.StageKey)
	if item.Value() != initialValue {
		t.Errorf("Initial value mismatch: got %q, want %q", item.Value(), initialValue)
	}

	// Change the environment value
	currentValue = updatedValue

	// Call Update
	cfg.Update(context.Background())

	// Verify the value was updated
	item = cfg.Get(common.StageKey)
	if item.Value() != updatedValue {
		t.Errorf("Updated value mismatch: got %q, want %q", item.Value(), updatedValue)
	}
}

func TestEnvConfigUpdateWithEmptyValue(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		return ""
	}

	cfg := NewEnvConfig(getenv)

	// Access a key to populate the items map
	_ = cfg.Get(common.StageKey)

	// Update should log a warning but not panic
	cfg.Update(context.Background())

	// Verify the item still exists but with empty value
	item := cfg.Get(common.StageKey)
	if item.Value() != "" {
		t.Errorf("Expected empty value, got %q", item.Value())
	}
}
