package puzzle

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"testing"
)

func newVerifyPayloadBytes(t *testing.T, challenge Challenge) ([]byte, *ComputePuzzle, *Salt) {
	t.Helper()

	p, err := NewComputePuzzleForChallenge(123, [PropertyIDSize]byte{1}, 0, challenge)
	if err != nil {
		t.Fatal(err)
	}
	salt := NewSalt([]byte("test-salt"))
	puzzlePayload, err := p.Serialize(t.Context(), salt, nil)
	if err != nil {
		t.Fatal(err)
	}

	var payload bytes.Buffer
	payload.WriteString(emptySolutions(p.SolutionsCount()).String())
	payload.WriteByte('.')
	if err := puzzlePayload.Write(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Bytes(), p, salt
}

func TestSolutionsArgon2IDSignedRoundTrip(t *testing.T) {
	p, err := NewComputePuzzleForChallenge(123, [PropertyIDSize]byte{1}, 0, ChallengeArgon2ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Init(DefaultValidityPeriod); err != nil {
		t.Fatal(err)
	}
	solutions, err := (&ComputeSolver{}).Solve(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	salt := NewSalt([]byte("test-salt"))
	signed, err := p.Serialize(t.Context(), salt, nil)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	encoded.WriteString(solutions.String())
	encoded.WriteByte('.')
	if err := signed.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	vp, err := ParseVerifyPayload[ComputePuzzle, *ComputePuzzle](t.Context(), encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := vp.VerifySignature(t.Context(), salt, nil); err != nil {
		t.Fatal(err)
	}
	if _, result := vp.VerifySolutions(t.Context()); result != VerifyNoError {
		t.Fatalf("verification = %v, want %v", result, VerifyNoError)
	}

	parsed := vp.puzzle.(*ComputePuzzle)
	parsed.solutionsCount++
	if _, result := vp.VerifySolutions(t.Context()); result == VerifyNoError {
		t.Fatal("wrong signed Argon solution count passed verification")
	}
}

func mutateEncodedPayloadPart(t *testing.T, payload []byte, part int, mutate func([]byte) []byte) []byte {
	t.Helper()

	parts := bytes.Split(bytes.Clone(payload), dotBytes)
	if len(parts) != 3 {
		t.Fatalf("payload parts = %d, want 3", len(parts))
	}
	decoded, err := base64.StdEncoding.DecodeString(string(parts[part]))
	if err != nil {
		t.Fatal(err)
	}
	parts[part] = []byte(base64.StdEncoding.EncodeToString(mutate(decoded)))
	return bytes.Join(parts, dotBytes)
}

func aliasBase64Padding(t *testing.T, encoded []byte) []byte {
	t.Helper()

	aliased := bytes.Clone(encoded)
	padding := 0
	for aliased[len(aliased)-1-padding] == '=' {
		padding++
	}
	if padding == 0 {
		t.Fatal("base64 value has no padding")
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	index := len(aliased) - padding - 1
	value := bytes.IndexByte([]byte(alphabet), aliased[index])
	if value < 0 || value == len(alphabet)-1 {
		t.Fatal("cannot alias base64 padding bits")
	}
	aliased[index] = alphabet[value+1]
	return aliased
}

func TestVerifyErrorString(t *testing.T) {
	t.Parallel()

	for i := VerifyError(0); i < VERIFY_ERRORS_COUNT; i++ {
		name := i.String()
		if name == "" {
			t.Errorf("Empty string for VerifyError(%d)", i)
		}

		if name == "error" {
			t.Errorf("Unknown VerifyError: %d", i)
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

func TestParseVerifyPayloadRejectsNonCanonicalComponents(t *testing.T) {
	t.Parallel()

	payload, _, _ := newVerifyPayloadBytes(t, ChallengeArgon2ID)
	if _, err := ParseVerifyPayload[ComputePuzzle](t.Context(), payload); err != nil {
		t.Fatalf("valid payload failed: %v", err)
	}

	tests := []struct {
		name string
		part int
		edit func([]byte) []byte
	}{
		{"PuzzleTrailingData", 1, func(data []byte) []byte { return append(data, 0) }},
		{"SignatureVersion", 2, func(data []byte) []byte { data[0]++; return data }},
		{"SignatureFlags", 2, func(data []byte) []byte { data[1] = 1; return data }},
		{"SignatureTrailingData", 2, func(data []byte) []byte { return append(data, 0) }},
		{"MetadataVersion", 0, func(data []byte) []byte { data[0]++; return data }},
		{"SolutionsUnderCount", 0, func(data []byte) []byte { return data[:len(data)-SolutionLength] }},
		{"SolutionsOverCount", 0, func(data []byte) []byte { return append(data, make([]byte, SolutionLength)...) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalid := mutateEncodedPayloadPart(t, payload, tt.part, tt.edit)
			if _, err := ParseVerifyPayload[ComputePuzzle](t.Context(), invalid); err == nil {
				t.Fatal("non-canonical payload parsed successfully")
			}
		})
	}
}

func TestParseVerifyPayloadAcceptsEquivalentBase64(t *testing.T) {
	t.Parallel()

	payload, _, _ := newVerifyPayloadBytes(t, ChallengeBlake2b)
	parts := bytes.Split(payload, dotBytes)
	tests := []struct {
		name string
		part int
		edit func([]byte) []byte
	}{
		{"PuzzlePaddingBits", 1, func(data []byte) []byte { return aliasBase64Padding(t, data) }},
		{"SignaturePaddingBits", 2, func(data []byte) []byte { return aliasBase64Padding(t, data) }},
		{"SolutionsPaddingBits", 0, func(data []byte) []byte { return aliasBase64Padding(t, data) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalidParts := make([][]byte, len(parts))
			for i := range parts {
				invalidParts[i] = bytes.Clone(parts[i])
			}
			invalidParts[tt.part] = tt.edit(invalidParts[tt.part])
			if _, err := ParseVerifyPayload[ComputePuzzle](t.Context(), bytes.Join(invalidParts, dotBytes)); err != nil {
				t.Fatalf("equivalent base64 payload failed: %v", err)
			}
		})
	}
}

func TestVerifyPayloadRejectsWrongCountBeforeHashing(t *testing.T) {
	t.Parallel()

	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 1)
	for _, count := range []int{p.SolutionsCount() - 1, p.SolutionsCount() + 1} {
		vp := &VerifyPayload{
			puzzle:     p,
			puzzleData: nil,
			solutions: &Solutions{
				Buffer:   make([]byte, count*SolutionLength),
				Metadata: &Metadata{},
			},
		}
		_, result := vp.VerifySolutions(t.Context())
		if result != ParseResponseError {
			t.Fatalf("count %d result = %v, want %v", count, result, ParseResponseError)
		}
	}
}

func TestSignatureRejectsNonCanonicalData(t *testing.T) {
	t.Parallel()

	valid := append([]byte{signatureVersion, flagWithExtra, 42}, make([]byte, sha1.Size)...)
	var parsed signature
	if err := parsed.UnmarshalBinary(valid); err != nil {
		t.Fatalf("valid signature failed: %v", err)
	}

	tests := []struct {
		name string
		data func() []byte
	}{
		{"Short", func() []byte { return bytes.Clone(valid[:len(valid)-1]) }},
		{"TrailingData", func() []byte { return append(bytes.Clone(valid), 0) }},
		{"UnknownVersion", func() []byte { data := bytes.Clone(valid); data[0]++; return data }},
		{"UnknownFlags", func() []byte { data := bytes.Clone(valid); data[1] = 1; return data }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s signature
			if err := s.UnmarshalBinary(tt.data()); err == nil {
				t.Fatal("non-canonical signature parsed successfully")
			}
		})
	}
}

func TestSignatureRejectsTamperedChallenge(t *testing.T) {
	t.Parallel()

	payload, _, salt := newVerifyPayloadBytes(t, ChallengeArgon2ID)
	parts := bytes.Split(payload, dotBytes)
	puzzleData, err := base64.StdEncoding.DecodeString(string(parts[1]))
	if err != nil {
		t.Fatal(err)
	}
	signatureData, err := base64.StdEncoding.DecodeString(string(parts[2]))
	if err != nil {
		t.Fatal(err)
	}
	var s signature
	if err := s.UnmarshalBinary(signatureData); err != nil {
		t.Fatal(err)
	}

	puzzleData[1] = byte(ChallengeBlake2b)
	vp := &VerifyPayload{signature: &s, puzzleData: puzzleData}
	if err := vp.VerifySignature(t.Context(), salt, nil); err != errSignatureMismatch {
		t.Fatalf("tampered challenge error = %v, want %v", err, errSignatureMismatch)
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
