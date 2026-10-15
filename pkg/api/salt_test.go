package api

import (
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
)

func TestPuzzleSaltUpdate(t *testing.T) {
	const value = "test-api-salt"
	salt := NewPuzzleSalt(config.NewStaticValue(common.APISaltKey, value))
	if err := salt.Update(); err != nil {
		t.Fatal(err)
	}
	if got := string(salt.Value().Data()); got != value {
		t.Fatalf("salt data = %q, want %q", got, value)
	}
}

func TestPuzzleSaltUpdateRejectsEmpty(t *testing.T) {
	salt := NewPuzzleSalt(config.NewStaticValue(common.APISaltKey, ""))
	if err := salt.Update(); err == nil {
		t.Fatal("expected an error for an empty API salt")
	}
	if salt.Value() != nil {
		t.Fatal("empty API salt was installed")
	}
}

func TestPuzzleSaltUpdatePreservesValidSalt(t *testing.T) {
	value := "test-api-salt"
	cfg := config.NewEnvConfig(func(string) string { return value })
	salt := NewPuzzleSalt(cfg.Get(common.APISaltKey))
	if err := salt.Update(); err != nil {
		t.Fatal(err)
	}
	previous := salt.Value()

	value = ""
	cfg.Update(t.Context())
	if err := salt.Update(); err == nil {
		t.Fatal("expected an error for an empty API salt")
	}
	if salt.Value() != previous {
		t.Fatal("failed update replaced the valid API salt")
	}
}
