package api

import (
	"bytes"
	"encoding/hex"
	"strings"
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

func TestUserFingerprintKeyUpdate(t *testing.T) {
	value := strings.Repeat("ab", 32)
	key := NewUserFingerprintKey(config.NewStaticValue(common.UserFingerprintIVKey, value))
	if err := key.Update(); err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(key.Value()); got != value {
		t.Fatalf("user fingerprint key = %s, want %s", got, value)
	}
}

func TestUserFingerprintKeyUpdateRejectsEmpty(t *testing.T) {
	key := NewUserFingerprintKey(config.NewStaticValue(common.UserFingerprintIVKey, ""))
	if err := key.Update(); err == nil {
		t.Fatal("expected an error for an empty user fingerprint key")
	}
	if len(key.Value()) == 0 {
		t.Fatal("empty user fingerprint key was installed")
	}
}

func TestUserFingerprintKeyUpdatePreservesValidKey(t *testing.T) {
	value := strings.Repeat("ab", 32)
	cfg := config.NewEnvConfig(func(string) string { return value })
	key := NewUserFingerprintKey(cfg.Get(common.UserFingerprintIVKey))
	if err := key.Update(); err != nil {
		t.Fatal(err)
	}
	previous := key.Value()

	value = ""
	cfg.Update(t.Context())
	if err := key.Update(); err == nil {
		t.Fatal("expected an error for an empty user fingerprint key")
	}
	if !bytes.Equal(key.Value(), previous) {
		t.Fatal("failed update replaced the valid user fingerprint key")
	}
}

func TestUserFingerprintKeyUpdateRejectsInvalidHex(t *testing.T) {
	key := NewUserFingerprintKey(config.NewStaticValue(common.UserFingerprintIVKey, "not-hex"))
	if err := key.Update(); err == nil {
		t.Fatal("expected an error for an invalid hex user fingerprint key")
	}
}

func TestUserFingerprintKeyUpdateRejectsTooLong(t *testing.T) {
	key := NewUserFingerprintKey(config.NewStaticValue(common.UserFingerprintIVKey, strings.Repeat("ab", 65)))
	if err := key.Update(); err == nil {
		t.Fatal("expected an error for a too-long user fingerprint key")
	}
}
