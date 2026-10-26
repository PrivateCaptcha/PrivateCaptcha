package common

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
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

				ids := []int{worker, i, 0, id, worker}
				encodedArray := hasher.EncryptArray(ids)
				decodedArray, err := hasher.DecryptArray(encodedArray)
				if err != nil || !slices.Equal(decodedArray, ids) {
					t.Errorf("array round trip for %v: got %v, %v", ids, decodedArray, err)
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

func TestIDHasherArrayRoundTrip(t *testing.T) {
	longArray := make([]int, 256)
	for i := range longArray {
		longArray[i] = i
	}

	testCases := []struct {
		name string
		ids  []int
	}{
		{
			name: "Zero",
			ids:  []int{0},
		},
		{
			name: "Single",
			ids:  []int{12345},
		},
		{
			name: "Multiple",
			ids:  []int{0, 1, 10, 100, 1000, 12345, 999999},
		},
		{
			name: "Descending",
			ids:  []int{999999, 1000, 100, 10, 1, 0},
		},
		{
			name: "Repeated",
			ids:  []int{7, 0, 7, 0, 7},
		},
		{
			name: "MaximumInt",
			ids:  []int{math.MaxInt, 0, math.MaxInt - 1},
		},
		{
			name: "LongArray",
			ids:  longArray,
		},
	}

	for _, salt := range []string{"", "testsalt"} {
		t.Run("Salt="+salt, func(t *testing.T) {
			hasher := NewIDHasher(&stubConfigItemHash{
				value: salt,
			})
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					original := slices.Clone(tc.ids)
					encoded := hasher.EncryptArray(tc.ids)
					if encoded == "" {
						t.Fatal("EncryptArray returned an empty identifier")
					}
					if salt != "" && len(encoded) < 10 {
						t.Errorf("salted identifier %q is shorter than 10 characters", encoded)
					}
					if !slices.Equal(tc.ids, original) {
						t.Fatalf("EncryptArray mutated input: got %v, want %v", tc.ids, original)
					}

					decoded, err := hasher.DecryptArray(encoded)
					if err != nil {
						t.Fatalf("DecryptArray(%q) failed: %v", encoded, err)
					}
					if !slices.Equal(decoded, original) {
						t.Errorf("round trip: got %v, want %v", decoded, original)
					}
				})
			}
		})
	}
}

func TestIDHasherEncryptArrayPlaintext(t *testing.T) {
	testCases := []struct {
		name string
		ids  []int
		want string
	}{
		{
			name: "Nil",
			want: "",
		},
		{
			name: "Empty",
			ids:  []int{},
			want: "",
		},
		{
			name: "Zero",
			ids:  []int{0},
			want: "0",
		},
		{
			name: "Single",
			ids:  []int{12345},
			want: "12345",
		},
		{
			name: "Multiple",
			ids:  []int{12, 0, 34, 12},
			want: "12.0.34.12",
		},
		{
			name: "NegativeFirst",
			ids:  []int{-1, 2, 3},
			want: "-1.2.3",
		},
		{
			name: "NegativeMiddle",
			ids:  []int{1, -2, 3},
			want: "1.-2.3",
		},
		{
			name: "NegativeLast",
			ids:  []int{1, 2, -3},
			want: "1.2.-3",
		},
		{
			name: "IntegerBounds",
			ids:  []int{math.MinInt, math.MaxInt},
			want: strconv.Itoa(math.MinInt) + "." + strconv.Itoa(math.MaxInt),
		},
	}

	for _, salt := range []string{"", "testsalt"} {
		t.Run("Salt="+salt, func(t *testing.T) {
			hasher := NewIDHasher(&stubConfigItemHash{
				value: salt,
			})
			for _, tc := range testCases {
				if salt != "" && len(tc.ids) > 0 && !slices.ContainsFunc(tc.ids, func(id int) bool { return id < 0 }) {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					if got := hasher.EncryptArray(tc.ids); got != tc.want {
						t.Errorf("EncryptArray(%v) = %q, want %q", tc.ids, got, tc.want)
					}
					if salt == "" && len(tc.ids) > 0 {
						decoded, err := hasher.DecryptArray(tc.want)
						if err != nil || !slices.Equal(decoded, tc.ids) {
							t.Errorf("DecryptArray(%q) = %v, %v; want %v, nil", tc.want, decoded, err, tc.ids)
						}
					}
				})
			}
		})
	}
}

func TestIDHasherDecryptArrayWithoutSaltInvalidInput(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{})
	invalid := []string{
		"", ".", ".1", "1.", "1..2", "1...2",
		"abc", "1.two.3", "1.2x", "1,2", "1/2",
		" 1.2", "1.2 ", "1. 2", "1.\t2", "1.2\n",
		"--1.2", "1.+", "1.0x10", "1.1e2",
	}
	for _, hash := range invalid {
		t.Run(strconv.Quote(hash), func(t *testing.T) {
			ids, err := hasher.DecryptArray(hash)
			if !errors.Is(err, strconv.ErrSyntax) {
				t.Errorf("DecryptArray(%q) error = %v, want %v", hash, err, strconv.ErrSyntax)
			}
			if ids != nil {
				t.Errorf("DecryptArray(%q) returned partial result %v", hash, ids)
			}
		})
	}

	overflow := strings.Repeat("9", 32)
	for _, hash := range []string{overflow, "-" + overflow, "1." + overflow, "1.-" + overflow} {
		t.Run(hash, func(t *testing.T) {
			ids, err := hasher.DecryptArray(hash)
			if !errors.Is(err, strconv.ErrRange) {
				t.Errorf("DecryptArray(%q) error = %v, want %v", hash, err, strconv.ErrRange)
			}
			if ids != nil {
				t.Errorf("DecryptArray(%q) returned partial result %v", hash, ids)
			}
		})
	}
}

func TestIDHasherDecryptArrayInvalidHash(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{
		value: "testsalt",
	})
	valid := hasher.EncryptArray([]int{1, 2, 3})
	for _, hash := range []string{"", "invalid!@#", ".", "1.2.3", "-1.2", " " + valid, valid + "!"} {
		t.Run(strconv.Quote(hash), func(t *testing.T) {
			ids, err := hasher.DecryptArray(hash)
			if err == nil {
				t.Errorf("DecryptArray(%q) succeeded with %v", hash, ids)
			}
			if hash == "" && !errors.Is(err, errUnexpectedIdentifierLen) {
				t.Errorf("empty hash error = %v, want %v", err, errUnexpectedIdentifierLen)
			}
			if ids != nil {
				t.Errorf("DecryptArray(%q) returned result %v for invalid hash", hash, ids)
			}
		})
	}
}

func TestIDHasherDecryptArrayHasherError(t *testing.T) {
	wantErr := errors.New("hash initialization failed")
	hasher := &idHasher{
		hashIDErr: wantErr,
	}
	for _, hash := range []string{"", "123", "1.2.3", "invalid!@#"} {
		t.Run(strconv.Quote(hash), func(t *testing.T) {
			ids, err := hasher.DecryptArray(hash)
			if !errors.Is(err, wantErr) {
				t.Errorf("DecryptArray(%q) error = %v, want %v", hash, err, wantErr)
			}
			if ids != nil {
				t.Errorf("DecryptArray(%q) returned %v with initialization error", hash, ids)
			}
		})
	}
}

func TestIDHasherArrayScalarCompatibility(t *testing.T) {
	for _, salt := range []string{"", "testsalt"} {
		t.Run("Salt="+salt, func(t *testing.T) {
			hasher := NewIDHasher(&stubConfigItemHash{
				value: salt,
			})
			for _, id := range []int{0, 1, 12345, math.MaxInt} {
				t.Run(strconv.Itoa(id), func(t *testing.T) {
					encoded := hasher.EncryptArray([]int{id})
					if want := hasher.Encrypt(id); encoded != want {
						t.Errorf("singleton array encoded as %q, want scalar encoding %q", encoded, want)
					}
					decoded, err := hasher.DecryptArray(hasher.Encrypt(id))
					if err != nil || !slices.Equal(decoded, []int{id}) {
						t.Errorf("array decode of scalar %d: got %v, %v", id, decoded, err)
					}
					decodedScalar, err := hasher.Decrypt(encoded)
					if err != nil || decodedScalar != id {
						t.Errorf("scalar decode of singleton %d: got %d, %v", id, decodedScalar, err)
					}
				})
			}
		})
	}
}

func TestIDHasherScalarDecryptRejectsArray(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{
		value: "testsalt",
	})
	encoded := hasher.EncryptArray([]int{1, 2, 3})
	if id, err := hasher.Decrypt(encoded); id != -1 || !errors.Is(err, errUnexpectedIdentifierLen) {
		t.Errorf("Decrypt(array) = %d, %v; want -1, %v", id, err, errUnexpectedIdentifierLen)
	}
	if id, err := hasher.Decrypt64(encoded); id != -1 || !errors.Is(err, errUnexpectedIdentifierLen) {
		t.Errorf("Decrypt64(array) = %d, %v; want -1, %v", id, err, errUnexpectedIdentifierLen)
	}
}

func TestIDHasherArraySalt(t *testing.T) {
	hasher := NewIDHasher(&stubConfigItemHash{
		value: "testsalt",
	})
	sameSalt := NewIDHasher(&stubConfigItemHash{
		value: "testsalt",
	})
	differentSalt := NewIDHasher(&stubConfigItemHash{
		value: "othersalt",
	})
	ids := []int{1, 0, 42, 1}
	encoded := hasher.EncryptArray(ids)
	if got := hasher.EncryptArray(ids); got != encoded {
		t.Errorf("repeated encoding = %q, want %q", got, encoded)
	}
	if got := sameSalt.EncryptArray(ids); got != encoded {
		t.Errorf("same salt encoding = %q, want %q", got, encoded)
	}
	if got := differentSalt.EncryptArray(ids); got == encoded {
		t.Errorf("different salts produced the same identifier %q", got)
	}
	if decoded, err := sameSalt.DecryptArray(encoded); err != nil || !slices.Equal(decoded, ids) {
		t.Errorf("same salt decode = %v, %v; want %v, nil", decoded, err, ids)
	}
	if decoded, err := differentSalt.DecryptArray(encoded); err == nil {
		t.Errorf("different salt decode succeeded with %v", decoded)
	}
}
