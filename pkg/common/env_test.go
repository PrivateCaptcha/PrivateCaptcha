package common

import (
	"os"
	"testing"
)

func TestNewEnvMapFromFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "env_test_*.env")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	envContent := `STAGE=dev
PC_LOCAL_ADDRESS=localhost:9090
PC_PORTAL_BASE_URL=portal.privatecaptcha.local
PC_API_BASE_URL=api.privatecaptcha.local
PC_ADMIN_EMAIL=admin@privatecaptcha.local`

	if _, err := tmpFile.WriteString(envContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	envMap, err := NewEnvMap(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to create env map from file: %v", err)
	}

	tests := []struct {
		key      string
		expected string
	}{
		{"STAGE", "dev"},
		{"PC_LOCAL_ADDRESS", "localhost:9090"},
		{"PC_PORTAL_BASE_URL", "portal.privatecaptcha.local"},
		{"PC_API_BASE_URL", "api.privatecaptcha.local"},
		{"PC_ADMIN_EMAIL", "admin@privatecaptcha.local"},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			actual := envMap.Get(tc.key)
			if actual != tc.expected {
				t.Errorf("Expected %s for key %s, got %s", tc.expected, tc.key, actual)
			}
		})
	}
}

func TestEnvMapGetEx(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "env_test_*.env")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	envContent := `STAGE=dev`

	if _, err := tmpFile.WriteString(envContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	envMap, err := NewEnvMap(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to create env map from file: %v", err)
	}

	value, ok := envMap.GetEx("STAGE")
	if !ok {
		t.Error("Expected key STAGE to exist")
	}
	if value != "dev" {
		t.Errorf("Expected 'dev', got '%s'", value)
	}

	_, ok = envMap.GetEx("NON_EXISTENT_KEY")
	if ok {
		t.Error("Expected key NON_EXISTENT_KEY to not exist")
	}

	_, ok = envMap.GetEx("")
	if ok {
		t.Error("Expected empty key to not return value")
	}
}

func TestEnvMapUpdate(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "env_test_*.env")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	envContent := `STAGE=dev`

	if _, err := tmpFile.WriteString(envContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	envMap, err := NewEnvMap(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to create env map from file: %v", err)
	}

	if err := envMap.Update(); err != nil {
		t.Errorf("Update failed: %v", err)
	}

	value := envMap.Get("STAGE")
	if value != "dev" {
		t.Errorf("Expected 'dev' after update, got '%s'", value)
	}
}

func TestEnvMapEmptyPath(t *testing.T) {
	envMap, err := NewEnvMap("")
	if err != nil {
		t.Fatalf("Failed to create env map with empty path: %v", err)
	}

	if envMap.envMap != nil {
		t.Error("Expected envMap to be nil for empty path")
	}

	if err := envMap.Update(); err != nil {
		t.Errorf("Update with empty path should not error: %v", err)
	}
}
