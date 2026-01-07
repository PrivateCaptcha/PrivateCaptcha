package common

import (
	"testing"
)

type stubConfigItemHash struct {
	key   ConfigKey
	value string
}

func (s *stubConfigItemHash) Key() ConfigKey { return s.key }
func (s *stubConfigItemHash) Value() string  { return s.value }

func TestIDHasherEncryptDecrypt(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{key: 0, value: "testsalt"})

	testCases := []int{0, 1, 10, 100, 1000, 12345, 999999}

	for _, id := range testCases {
		encrypted := hasher.Encrypt(id)
		if len(encrypted) == 0 {
			t.Errorf("Encrypted value is empty for id %d", id)
			continue
		}

		decrypted, err := hasher.Decrypt(encrypted)
		if err != nil {
			t.Errorf("Failed to decrypt %s: %v", encrypted, err)
			continue
		}

		if decrypted != id {
			t.Errorf("Decrypted value %d does not match original %d", decrypted, id)
		}
	}
}

func TestIDHasherEncrypt64Decrypt64(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{key: 0, value: "testsalt"})

	testCases := []int64{0, 1, 10, 100, 1000, 12345, 999999999999}

	for _, id := range testCases {
		encrypted := hasher.Encrypt64(id)
		if len(encrypted) == 0 {
			t.Errorf("Encrypted value is empty for id %d", id)
			continue
		}

		decrypted, err := hasher.Decrypt64(encrypted)
		if err != nil {
			t.Errorf("Failed to decrypt %s: %v", encrypted, err)
			continue
		}

		if decrypted != id {
			t.Errorf("Decrypted value %d does not match original %d", decrypted, id)
		}
	}
}

func TestIDHasherWithoutSalt(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{key: 0, value: ""})

	id := 12345
	encrypted := hasher.Encrypt(id)
	if encrypted != "12345" {
		t.Errorf("Without salt, Encrypt should return plain number string, got %s", encrypted)
	}

	decrypted, err := hasher.Decrypt("12345")
	if err != nil {
		t.Errorf("Failed to decrypt: %v", err)
	}
	if decrypted != id {
		t.Errorf("Decrypted value %d does not match original %d", decrypted, id)
	}
}

func TestIDHasherDecryptInvalidHash(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{key: 0, value: "testsalt"})

	_, err := hasher.Decrypt("invalid!@#")
	if err == nil {
		t.Error("Expected error for invalid hash, got nil")
	}
}

func TestIDHasherDecrypt64InvalidHash(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{key: 0, value: "testsalt"})

	_, err := hasher.Decrypt64("invalid!@#")
	if err == nil {
		t.Error("Expected error for invalid hash, got nil")
	}
}
