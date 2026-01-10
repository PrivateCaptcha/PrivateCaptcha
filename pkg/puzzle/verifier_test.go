package puzzle

import (
	"context"
	"encoding/base64"
	"testing"
)

func TestVerifyErrorString(t *testing.T) {
	t.Parallel()

	// Test all VerifyError values from 0 to VERIFY_ERRORS_COUNT
	expectedStrings := map[VerifyError]string{
		VerifyNoError:           "no-error",
		VerifyErrorOther:        "error-other",
		DuplicateSolutionsError: "solution-duplicates",
		InvalidSolutionError:    "solution-invalid",
		ParseResponseError:      "solution-bad-format",
		PuzzleExpiredError:      "puzzle-expired",
		InvalidPropertyError:    "property-invalid",
		WrongOwnerError:         "property-owner-mismatch",
		VerifiedBeforeError:     "solution-verified-before",
		MaintenanceModeError:    "maintenance-mode",
		TestPropertyError:       "property-test",
		IntegrityError:          "integrity-error",
		OrgScopeError:           "property-org-scope",
	}

	for i := VerifyError(0); i < VERIFY_ERRORS_COUNT; i++ {
		name := i.String()
		if name == "" {
			t.Errorf("Empty string for VerifyError(%d)", i)
		}

		if expected, ok := expectedStrings[i]; ok {
			if name != expected {
				t.Errorf("VerifyError(%d).String() = %q, want %q", i, name, expected)
			}
		}
	}
}

func TestVerifyErrorStringUnknown(t *testing.T) {
	t.Parallel()

	// Test an unknown VerifyError value
	unknown := VerifyError(999)
	if unknown.String() != "error" {
		t.Errorf("Expected 'error' for unknown VerifyError, got %q", unknown.String())
	}
}

func TestParseVerifyPayloadEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := ParseVerifyPayload[ComputePuzzle, *ComputePuzzle](ctx, []byte{})

	if err != errPayloadEmpty {
		t.Errorf("Expected errPayloadEmpty, got %v", err)
	}
}

func TestParseVerifyPayloadWrongPartsNumber(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	testCases := []struct {
		name    string
		payload []byte
	}{
		{"no_dots", []byte("nodots")},
		{"one_dot", []byte("one.dot")},
		{"three_dots", []byte("one.two.three.four")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseVerifyPayload[ComputePuzzle, *ComputePuzzle](ctx, tc.payload)
			if err != errWrongPartsNumber {
				t.Errorf("Expected errWrongPartsNumber, got %v", err)
			}
		})
	}
}

func TestParseVerifyPayloadEmptyParts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	testCases := []struct {
		name    string
		payload []byte
	}{
		{"empty_solutions", []byte(".puzzle.signature")},
		{"empty_puzzle", []byte("solutions..signature")},
		{"empty_signature", []byte("solutions.puzzle.")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseVerifyPayload[ComputePuzzle, *ComputePuzzle](ctx, tc.payload)
			if err != errEmptyPayloadPart {
				t.Errorf("Expected errEmptyPayloadPart, got %v", err)
			}
		})
	}
}

func TestParseVerifyPayloadInvalidBase64Puzzle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Invalid base64 in puzzle part
	payload := []byte("solutions.!@#$%invalid.signature")
	_, err := ParseVerifyPayload[ComputePuzzle, *ComputePuzzle](ctx, payload)

	if err == nil {
		t.Error("Expected error for invalid base64 puzzle")
	}
}

func TestParseVerifyPayloadInvalidBase64Signature(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Valid puzzle but invalid signature
	validPuzzle := base64.StdEncoding.EncodeToString([]byte("valid puzzle data"))
	payload := []byte("solutions." + validPuzzle + ".!@#$%invalid")
	_, err := ParseVerifyPayload[ComputePuzzle, *ComputePuzzle](ctx, payload)

	if err == nil {
		t.Error("Expected error for invalid base64 signature")
	}
}

func TestParseVerifyPayloadEmptyDecodedPuzzle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Empty string encoded in base64 will decode to empty bytes
	emptyB64 := base64.StdEncoding.EncodeToString([]byte{})
	validSig := base64.StdEncoding.EncodeToString([]byte("sig"))
	payload := []byte("solutions." + emptyB64 + "." + validSig)

	// Note: base64.StdEncoding.EncodeToString([]byte{}) returns "" which when
	// re-decoded gives empty bytes
	_, err := ParseVerifyPayload[ComputePuzzle, *ComputePuzzle](ctx, payload)

	// The second part will be empty when split, so this returns errEmptyPayloadPart
	if err == nil || (err != errEmptyPayloadPart && err != errEmptyPuzzle) {
		t.Errorf("Expected errEmptyPayloadPart or errEmptyPuzzle, got %v", err)
	}
}

func TestParseVerifyPayloadInvalidPuzzleBytes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Valid base64 but invalid puzzle data
	invalidPuzzle := base64.StdEncoding.EncodeToString([]byte("too short"))
	validSig := base64.StdEncoding.EncodeToString([]byte("valid sig bytes here"))
	payload := []byte("solutions." + invalidPuzzle + "." + validSig)

	_, err := ParseVerifyPayload[ComputePuzzle, *ComputePuzzle](ctx, payload)

	// Should fail to unmarshal the puzzle
	if err == nil {
		t.Error("Expected error for invalid puzzle bytes")
	}
}

func TestVerifyPayloadVerifySignatureFingerprintMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	salt := NewSalt([]byte("test-salt"))

	// Create a verify payload with mismatched fingerprint
	vp := &VerifyPayload{
		signature: &signature{
			Fingerprint: salt.Fingerprint() + 1, // Wrong fingerprint
			Hash:        []byte("some hash"),
		},
		puzzleData: []byte("puzzle data"),
	}

	err := vp.VerifySignature(ctx, salt, nil)

	if err != ErrSignKeyMismatch {
		t.Errorf("Expected ErrSignKeyMismatch, got %v", err)
	}
}

func TestVerifyPayloadVerifySignatureHashMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	salt := NewSalt([]byte("test-salt"))

	// Create a verify payload with correct fingerprint but wrong hash
	vp := &VerifyPayload{
		signature: &signature{
			Fingerprint: salt.Fingerprint(),
			Hash:        []byte("wrong hash"),
			Flags:       0, // No extra salt
		},
		puzzleData: []byte("puzzle data"),
	}

	err := vp.VerifySignature(ctx, salt, nil)

	if err != errSignatureMismatch {
		t.Errorf("Expected errSignatureMismatch, got %v", err)
	}
}

func TestStubVerifyPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	puzzle := new(ComputePuzzle)
	stub := NewStubPayload(puzzle)

	// Test VerifySolutions
	meta, verr := stub.VerifySolutions(ctx)
	if verr != TestPropertyError {
		t.Errorf("Expected TestPropertyError, got %v", verr)
	}
	if meta == nil {
		t.Error("Expected non-nil metadata")
	}

	// Test Puzzle
	if stub.Puzzle() != puzzle {
		t.Error("Puzzle() should return the same puzzle")
	}

	// Test NeedsExtraSalt
	if stub.NeedsExtraSalt() {
		t.Error("Stub should not need extra salt")
	}

	// Test VerifySignature
	err := stub.VerifySignature(ctx, nil, nil)
	if err != errStubPayload {
		t.Errorf("Expected errStubPayload, got %v", err)
	}
}

func TestVerifyPayloadNeedsExtraSalt(t *testing.T) {
	t.Parallel()

	vp := &VerifyPayload{
		signature: &signature{
			Flags: flagWithExtra,
		},
	}

	if !vp.NeedsExtraSalt() {
		t.Error("Expected NeedsExtraSalt to return true")
	}

	vp.signature.Flags = 0
	if vp.NeedsExtraSalt() {
		t.Error("Expected NeedsExtraSalt to return false")
	}
}
