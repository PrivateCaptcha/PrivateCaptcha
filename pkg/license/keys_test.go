package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestActivationKeys(t *testing.T) {
	t.Parallel()

	keys, err := ActivationKeys()
	if err != nil {
		t.Fatalf("Failed to get activation keys: %v", err)
	}

	// We should have at least 0 or more keys (empty.keys file may be there)
	// The function should not error even if no keys are present
	_ = keys
}

func TestActivationKeysFromHardcoded(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	hardcodedKeys := []*hardcodedKey{
		{
			KeyID:   1,
			KeyData: base64.StdEncoding.EncodeToString(pubKey),
		},
		{
			KeyID:   2,
			KeyData: base64.StdEncoding.EncodeToString(pubKey),
		},
	}

	keys, err := activationKeys(hardcodedKeys)
	if err != nil {
		t.Fatalf("Failed to convert hardcoded keys: %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}

	if keys[0].ID != 1 {
		t.Errorf("Expected key ID 1, got %d", keys[0].ID)
	}

	if keys[1].ID != 2 {
		t.Errorf("Expected key ID 2, got %d", keys[1].ID)
	}
}

func TestActivationKeysFromHardcodedInvalidBase64(t *testing.T) {
	t.Parallel()

	hardcodedKeys := []*hardcodedKey{
		{
			KeyID:   1,
			KeyData: "not-valid-base64!@#$",
		},
	}

	_, err := activationKeys(hardcodedKeys)
	if err == nil {
		t.Error("Expected error for invalid base64 key data")
	}
}

func TestActivationKeysEmptyInput(t *testing.T) {
	t.Parallel()

	keys, err := activationKeys([]*hardcodedKey{})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if len(keys) != 0 {
		t.Errorf("Expected 0 keys, got %d", len(keys))
	}
}

func TestActivationKeyStructure(t *testing.T) {
	t.Parallel()

	pubKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	key := &ActivationKey{
		ID:   42,
		Data: pubKey,
	}

	if key.ID != 42 {
		t.Errorf("Expected ID 42, got %d", key.ID)
	}

	if len(key.Data) != ed25519.PublicKeySize {
		t.Errorf("Expected key data length %d, got %d", ed25519.PublicKeySize, len(key.Data))
	}
}
