package common

import (
	"sync"
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

func TestIDHasherConcurrentReuse(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{value: "testsalt"}).(*idHasher)
	if hasher.hashID == nil {
		t.Fatal("expected a cached HashID for a nonempty salt")
	}

	const workers = 32
	const iterations = 100
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := worker*iterations + i
				encoded := hasher.Encrypt(id)
				decoded, err := hasher.Decrypt(encoded)
				if err != nil || decoded != id {
					t.Errorf("int round trip for %d: got %d, %v", id, decoded, err)
				}

				id64 := int64(id) + 1<<40
				encoded64 := hasher.Encrypt64(id64)
				decoded64, err := hasher.Decrypt64(encoded64)
				if err != nil || decoded64 != id64 {
					t.Errorf("int64 round trip for %d: got %d, %v", id64, decoded64, err)
				}
			}
		}(worker)
	}
	wg.Wait()
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
