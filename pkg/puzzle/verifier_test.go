package puzzle

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"testing"
	"time"
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
	solutions.Metadata.elapsedMillis = 0
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
	if _, result := vp.VerifySolutions(t.Context(), new(100*time.Millisecond)); result != VerifyNoError {
		t.Fatalf("verification = %v, want %v", result, VerifyNoError)
	}

	parsed := vp.puzzle.(*ComputePuzzle)
	parsed.solutionsCount++
	if _, result := vp.VerifySolutions(t.Context(), nil); result == VerifyNoError {
		t.Fatal("wrong signed Argon solution count passed verification")
	}
}

func TestVerifyPayloadSolveTime(t *testing.T) {
	for _, difficulty := range []uint8{152, 168} {
		t.Run(fmt.Sprintf("Difficulty%d", difficulty), func(t *testing.T) {
			p := NewComputePuzzle(123, [PropertyIDSize]byte{1}, difficulty)
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

			for _, tc := range []struct {
				name    string
				elapsed uint32
				want    VerifyError
			}{
				{
					name:    "Zero",
					elapsed: 0,
					want:    SolutionTooFastError,
				},
				{
					name:    "FarBelowMinimum",
					elapsed: 2,
					want:    SolutionTooFastError,
				},
				{
					name:    "BelowMinimum",
					elapsed: 99,
					want:    SolutionTooFastError,
				},
				{
					name:    "AboveMinimum",
					elapsed: 100,
					want:    VerifyNoError,
				},
				{
					name:    "Normal",
					elapsed: 325,
					want:    VerifyNoError,
				},
				{
					name:    "Slower",
					elapsed: 1000,
					want:    VerifyNoError,
				},
				{
					name:    "Maximum",
					elapsed: ^uint32(0),
					want:    VerifyNoError,
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					tampered := mutateEncodedPayloadPart(t, encoded.Bytes(), 0, func(data []byte) []byte {
						binary.LittleEndian.PutUint32(data[3:metadataLength], tc.elapsed)
						return data
					})
					vp, err := ParseVerifyPayload[ComputePuzzle](t.Context(), tampered)
					if err != nil {
						t.Fatal(err)
					}
					if err := vp.VerifySignature(t.Context(), salt, nil); err != nil {
						t.Fatal(err)
					}
					metadata, result := vp.VerifySolutions(t.Context(), new(100*time.Millisecond))
					want := tc.want
					if difficulty == 152 {
						want = VerifyNoError
					}
					if result != want || metadata.ElapsedMillis() != tc.elapsed {
						t.Fatalf("verification = %v, elapsed = %d; want %v, %d", result, metadata.ElapsedMillis(), want, tc.elapsed)
					}
				})
			}

			solutions.Metadata.elapsedMillis = 0
			copy(solutions.Buffer[SolutionLength:2*SolutionLength], solutions.Buffer[:SolutionLength])
			body, err := p.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			vp := &VerifyPayload{
				puzzle:     p,
				puzzleData: body,
				solutions:  solutions,
			}
			if _, result := vp.VerifySolutions(t.Context(), new(100*time.Millisecond)); result != DuplicateSolutionsError {
				t.Fatalf("duplicate solutions with zero elapsed time = %v, want %v", result, DuplicateSolutionsError)
			}
		})
	}
}

func TestTooFastVerificationResult(t *testing.T) {
	result := NewVerifyResult(VerifyError(13))
	if !result.Success() {
		t.Fatal("too-fast verification must still succeed")
	}
	if result.Error.String() != "solution-too-fast" {
		t.Fatalf("too-fast error string = %q", result.Error.String())
	}
	if codes := result.ErrorsToStrings(); len(codes) != 1 || codes[0] != "solution-too-fast" {
		t.Fatalf("too-fast error codes = %v", codes)
	}
}

func TestVerifySolveTimeBuckets(t *testing.T) {
	for _, tc := range []struct {
		difficulty uint8
		fastMillis uint32
	}{
		{
			difficulty: 168,
			fastMillis: 99,
		},
		{
			difficulty: 169,
			fastMillis: 109,
		},
		{
			difficulty: 176,
			fastMillis: 199,
		},
		{
			difficulty: 183,
			fastMillis: 366,
		},
		{
			difficulty: 184,
			fastMillis: 399,
		},
		{
			difficulty: 192,
			fastMillis: 799,
		},
		{
			difficulty: 199,
			fastMillis: 1467,
		},
		{
			difficulty: 200,
			fastMillis: 1599,
		},
		{
			difficulty: 255,
			fastMillis: 1599,
		},
	} {
		t.Run(fmt.Sprintf("Difficulty%d", tc.difficulty), func(t *testing.T) {
			p := NewComputePuzzle(123, [PropertyIDSize]byte{}, tc.difficulty)
			metadata := &Metadata{
				elapsedMillis: tc.fastMillis,
			}
			if result := verifySolveTime(p, metadata, new(100*time.Millisecond)); result != SolutionTooFastError {
				t.Fatalf("%dms verification = %v, want %v", metadata.elapsedMillis, result, SolutionTooFastError)
			}
			metadata.elapsedMillis++
			if result := verifySolveTime(p, metadata, new(100*time.Millisecond)); result != VerifyNoError {
				t.Fatalf("%dms verification = %v, want %v", metadata.elapsedMillis, result, VerifyNoError)
			}
		})
	}
}

func TestVerifySolveTimeWithoutBaseline(t *testing.T) {
	for _, challenge := range []Challenge{ChallengeBlake2b, ChallengeArgon2ID} {
		for _, difficulty := range []uint8{0, 1, 135} {
			p, err := NewComputePuzzleForChallenge(123, [PropertyIDSize]byte{}, difficulty, challenge)
			if err != nil {
				t.Fatal(err)
			}
			if result := verifySolveTime(p, nil, new(100*time.Millisecond)); result != VerifyNoError {
				t.Fatalf("challenge %d, difficulty %d, zero elapsed = %v", challenge, difficulty, result)
			}
			metadata := &Metadata{
				elapsedMillis: 1,
			}
			if result := verifySolveTime(p, metadata, new(100*time.Millisecond)); result != VerifyNoError {
				t.Fatalf("challenge %d, difficulty %d, positive elapsed = %v", challenge, difficulty, result)
			}
		}
	}
	zero := NewComputePuzzle(0, [PropertyIDSize]byte{}, 0)
	if result := verifySolveTime(zero, nil, new(100*time.Millisecond)); result != VerifyNoError {
		t.Fatalf("zero puzzle timing = %v, want %v", result, VerifyNoError)
	}
}

func TestVerifySolveTimeSubsecondBenchmarks(t *testing.T) {
	for _, difficulty := range []uint8{136, 151, 152, 167} {
		p := NewComputePuzzle(123, [PropertyIDSize]byte{}, difficulty)
		for _, elapsed := range []uint32{0, 1, 1000} {
			metadata := &Metadata{
				elapsedMillis: elapsed,
			}
			for _, minimum := range []time.Duration{100 * time.Millisecond, time.Hour} {
				if result := verifySolveTime(p, metadata, &minimum); result != VerifyNoError {
					t.Fatalf("difficulty %d, elapsed %dms, minimum %v: got %v, want %v", difficulty, elapsed, minimum, result, VerifyNoError)
				}
			}
		}
	}
}

func TestVerifySolveTimeBenchmarkCutoff(t *testing.T) {
	previous := blake2bSolveTimeSeconds[0]
	t.Cleanup(func() { blake2bSolveTimeSeconds[0] = previous })
	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 136)
	for _, tc := range []struct {
		seconds float64
		want    VerifyError
	}{
		{
			seconds: 0.999999999,
			want:    VerifyNoError,
		},
		{
			seconds: 1,
			want:    SolutionTooFastError,
		},
		{
			seconds: 1.000000001,
			want:    SolutionTooFastError,
		},
	} {
		blake2bSolveTimeSeconds[0] = tc.seconds
		for _, elapsed := range []uint32{0, 1} {
			metadata := &Metadata{
				elapsedMillis: elapsed,
			}
			if result := verifySolveTime(p, metadata, new(100*time.Millisecond)); result != tc.want {
				t.Fatalf("benchmark %vs, elapsed %dms: got %v, want %v", tc.seconds, elapsed, result, tc.want)
			}
		}
	}
}

func TestVerifyMinSolveTimeAtMedium(t *testing.T) {
	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 168)
	metadata := &Metadata{
		elapsedMillis: 100,
	}
	for _, minimum := range []time.Duration{50 * time.Millisecond, 100 * time.Millisecond} {
		if result := verifySolveTime(p, metadata, &minimum); result != VerifyNoError {
			t.Fatalf("minimum %v: result = %v, want %v", minimum, result, VerifyNoError)
		}
	}
	for _, minimum := range []time.Duration{100*time.Millisecond + time.Nanosecond, 150 * time.Millisecond, time.Second} {
		if result := verifySolveTime(p, metadata, &minimum); result != SolutionTooFastError {
			t.Fatalf("minimum %v: result = %v, want %v", minimum, result, SolutionTooFastError)
		}
	}
	higher := NewComputePuzzle(123, [PropertyIDSize]byte{}, 255)
	if result := verifySolveTime(higher, metadata, new(time.Duration(1<<63-1))); result != SolutionTooFastError {
		t.Fatalf("maximum duration: result = %v, want %v", result, SolutionTooFastError)
	}
	metadata.elapsedMillis = 0
	if result := verifySolveTime(p, metadata, new(time.Millisecond)); result != SolutionTooFastError {
		t.Fatalf("zero elapsed bypassed minimum: %v", result)
	}
}

func TestVerifyNegativeElapsedMillis(t *testing.T) {
	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 168)
	for _, encoded := range []uint32{0x80000000, 0xffffffff} {
		data := make([]byte, metadataLength)
		data[0] = metadataVersion
		binary.LittleEndian.PutUint32(data[3:], encoded)
		metadata := &Metadata{}
		if err := metadata.UnmarshalBinary(data); err != nil {
			t.Fatal(err)
		}
		if result := verifySolveTime(p, metadata, new(time.Duration(1<<63-1))); result != VerifyNoError {
			t.Fatalf("negative encoded elapsed %x: got %v, want %v", encoded, result, VerifyNoError)
		}
	}
}

func TestVerifySolveTimeDisabled(t *testing.T) {
	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 168)
	for _, elapsed := range []uint32{0, 1, 99} {
		metadata := &Metadata{
			elapsedMillis: elapsed,
		}
		if result := verifySolveTime(p, metadata, nil); result != VerifyNoError {
			t.Fatalf("disabled heuristic, elapsed %dms: got %v, want %v", elapsed, result, VerifyNoError)
		}
	}
}

func TestVerifyInvalidSolutionsBeforeTiming(t *testing.T) {
	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 255)
	body, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	solutions := emptySolutions(p.SolutionsCount())
	for lane := range p.SolutionsCount() {
		solutions.Buffer[lane*SolutionLength] = byte(lane)
	}
	vp := &VerifyPayload{
		puzzle:     p,
		puzzleData: body,
		solutions:  solutions,
	}
	if _, result := vp.VerifySolutions(t.Context(), nil); result != InvalidSolutionError {
		t.Fatalf("invalid proof with zero elapsed = %v, want %v", result, InvalidSolutionError)
	}
}

func TestPuzzleEdgeTokenValiditySignedRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name      string
		challenge Challenge
	}{
		{
			name:      "Blake2b",
			challenge: ChallengeBlake2b,
		},
		{
			name:      "Argon2ID",
			challenge: ChallengeArgon2ID,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewComputePuzzleForChallenge(123, [PropertyIDSize]byte{1}, 0, tc.challenge)
			if err != nil {
				t.Fatal(err)
			}
			p.SetEdgeTokenValidity(30 * time.Minute)
			solutions, err := (&ComputeSolver{}).Solve(t.Context(), p)
			if err != nil {
				t.Fatal(err)
			}
			salt := NewSalt([]byte("test-salt"))
			extraSalt := []byte("property-salt")
			signed, err := p.Serialize(t.Context(), salt, extraSalt)
			if err != nil {
				t.Fatal(err)
			}
			var encoded bytes.Buffer
			encoded.WriteString(solutions.String())
			encoded.WriteByte('.')
			if err := signed.Write(&encoded); err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseVerifyPayload[ComputePuzzle](t.Context(), encoded.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if err := parsed.VerifySignature(t.Context(), salt, extraSalt); err != nil {
				t.Fatal(err)
			}
			if parsed.Puzzle().EdgeTokenValidity() != 30*time.Minute {
				t.Fatal("signed payload lost edge-token validity")
			}
			if _, result := parsed.VerifySolutions(t.Context(), nil); result != VerifyNoError {
				t.Fatalf("verification = %v, want %v", result, VerifyNoError)
			}
			tampered := mutateEncodedPayloadPart(t, encoded.Bytes(), 1, func(data []byte) []byte {
				data[32] ^= 1
				return data
			})
			modified, err := ParseVerifyPayload[ComputePuzzle](t.Context(), tampered)
			if err != nil {
				t.Fatal(err)
			}
			if err := modified.VerifySignature(t.Context(), salt, extraSalt); err != errSignatureMismatch {
				t.Fatalf("modified edge-token validity signature error = %v, want %v", err, errSignatureMismatch)
			}
		})
	}
}

func TestParseVerifyPayloadVariableUserData(t *testing.T) {
	for _, tc := range []struct {
		name      string
		challenge Challenge
		extra     int
	}{
		{"Blake2b", ChallengeBlake2b, 32},
		{"Blake2bAtNonceBoundary", ChallengeBlake2b, PuzzleBytesLength - SolutionLength - 47},
		{"Argon2ID", ChallengeArgon2ID, argon2IDPasswordSize - SolutionLength - puzzleV2MinSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			difficulty := uint8(0)
			if tc.challenge == ChallengeBlake2b {
				difficulty = 1
			}
			p, err := NewComputePuzzleForChallenge(123, [PropertyIDSize]byte{1}, difficulty, tc.challenge)
			if err != nil {
				t.Fatal(err)
			}
			p.userData = append(p.userData, bytes.Repeat([]byte{0xab}, tc.extra)...)
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

			parsed, err := ParseVerifyPayload[ComputePuzzle](t.Context(), encoded.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if err := parsed.VerifySignature(t.Context(), salt, nil); err != nil {
				t.Fatal(err)
			}
			if _, result := parsed.VerifySolutions(t.Context(), nil); result != VerifyNoError {
				t.Fatalf("verification = %v, want %v", result, VerifyNoError)
			}

			tampered := mutateEncodedPayloadPart(t, encoded.Bytes(), 1, func(data []byte) []byte {
				data[len(data)-1] ^= 1
				return data
			})
			modified, err := ParseVerifyPayload[ComputePuzzle](t.Context(), tampered)
			if err != nil {
				t.Fatal(err)
			}
			if err := modified.VerifySignature(t.Context(), salt, nil); err != errSignatureMismatch {
				t.Fatalf("modified user data signature error = %v, want %v", err, errSignatureMismatch)
			}
		})
	}
}

func TestParseVerifyPayloadBodyLimit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		challenge Challenge
	}{
		{"Blake2b", ChallengeBlake2b},
		{"Argon2ID", ChallengeArgon2ID},
	} {
		payload, _, _ := newVerifyPayloadBytes(t, tc.challenge)
		parts := bytes.Split(payload, dotBytes)
		body, err := base64.StdEncoding.DecodeString(string(parts[1]))
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, bytes.Repeat([]byte{0xab}, 256-len(body))...)
		for _, sizeCase := range []struct {
			name string
			body []byte
			ok   bool
		}{
			{"AtLimit", body, true},
			{"OverLimit", append(bytes.Clone(body), 0), false},
		} {
			t.Run(tc.name+sizeCase.name, func(t *testing.T) {
				parts[1] = []byte(base64.StdEncoding.EncodeToString(sizeCase.body))
				_, err := ParseVerifyPayload[ComputePuzzle](t.Context(), bytes.Join(parts, dotBytes))
				if (err == nil) != sizeCase.ok {
					t.Fatalf("%d-byte puzzle: error = %v, want success = %t", len(sizeCase.body), err, sizeCase.ok)
				}
			})
		}
	}
	payload, _, _ := newVerifyPayloadBytes(t, ChallengeBlake2b)
	parts := bytes.Split(payload, dotBytes)
	parts[1] = bytes.Repeat([]byte{'A'}, 256*1024)
	if _, err := ParseVerifyPayload[ComputePuzzle](t.Context(), bytes.Join(parts, dotBytes)); err != errInvalidEncoding {
		t.Fatalf("large encoded puzzle error = %v, want %v", err, errInvalidEncoding)
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
		_, result := vp.VerifySolutions(t.Context(), nil)
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
	meta, verr := stub.VerifySolutions(ctx, nil)
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
