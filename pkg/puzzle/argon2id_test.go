package puzzle

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"golang.org/x/crypto/argon2"
)

type argon2IDCompatibilityVector struct {
	Version     int
	Passes      uint32
	MemoryKiB   uint32
	Parallelism uint8
	Password    string
	Salt        string
	Tag         string
}

func BenchmarkArgon2IDVerification(b *testing.B) {
	body := make([]byte, puzzleV2Size)
	body[0] = puzzleVersion2
	body[1] = byte(ChallengeArgon2ID)
	memoryKiB := argon2IDMemoryKiB
	for solutionsCount := 1; solutionsCount <= 8; solutionsCount++ {
		body[27] = byte(solutionsCount)
		b.Run(fmt.Sprintf("%dKiB/%dSolutions", memoryKiB, solutionsCount), func(b *testing.B) {
			solutions := &Solutions{Buffer: make([]byte, solutionsCount*SolutionLength)}
			for solutionIndex := range solutionsCount {
				solutions.Buffer[solutionIndex*SolutionLength] = byte(solutionIndex)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := solutions.VerifyArgon2ID(b.Context(), body, 0, memoryKiB); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type argon2IDProjectVector struct {
	MemoryKiB  uint32
	Nonce      string
	PuzzleBody string
	Tag        string
	Value      uint32
}

type argon2IDProjectFixture struct {
	Version     int
	Passes      uint32
	Parallelism uint8
	TagLength   uint32
	Nonce       string
	PuzzleBody  string
	Vectors     []argon2IDProjectVector
	Transcripts []argon2IDProjectVector
}

type argon2IDFixture struct {
	RFCCompatible argon2IDCompatibilityVector
	Project       argon2IDProjectFixture
}

func decodeArgon2IDHex(t *testing.T, value string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func loadArgon2IDFixture(t *testing.T) argon2IDFixture {
	t.Helper()
	return argon2IDFixture{
		RFCCompatible: argon2IDCompatibilityVector{
			Version: 19, Passes: 1, MemoryKiB: 64, Parallelism: 1,
			Password: "70617373776f7264", Salt: "736f6d6573616c74",
			Tag: "655ad15eac652dc59f7170a7332bf49b8469be1fdb9c28bb",
		},
		Project: argon2IDProjectFixture{
			Version: 19, Passes: 1, Parallelism: 1, TagLength: 32,
			Nonce:      "0001020304050607",
			PuzzleBody: "0201000102030405060708090a0b0c0d0e0f0807060504030201981880857467101112131415161718191a1b1c1d1e1f",
			Vectors: []argon2IDProjectVector{{
				MemoryKiB: 16384, Tag: "ed16534ab875459775338b0027e9cce89153734c64fdbff6121d73511862b66f", Value: 1246959341,
			}},
			Transcripts: []argon2IDProjectVector{{
				MemoryKiB: 16384, Nonce: "f0e0d0c0b0a09080",
				PuzzleBody: "0201f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff1122334455667788880100f15365ffeeddccbbaa99887766554433221100",
				Tag:        "6ab6bde7102506d3b7156239571761f4e881d418d111ff11c7bca35e3322c66f", Value: 3887969898,
			}},
		},
	}
}

func TestArgon2ID(t *testing.T) {
	fixture := loadArgon2IDFixture(t)

	t.Run("RFCCompatibleVector", func(t *testing.T) {
		vector := fixture.RFCCompatible
		if vector.Version != argon2.Version {
			t.Fatalf("version = %d, want %d", vector.Version, argon2.Version)
		}

		expected := decodeArgon2IDHex(t, vector.Tag)
		actual := argon2.IDKey(
			decodeArgon2IDHex(t, vector.Password),
			decodeArgon2IDHex(t, vector.Salt),
			vector.Passes,
			vector.MemoryKiB,
			vector.Parallelism,
			uint32(len(expected)),
		)
		if !bytes.Equal(actual, expected) {
			t.Fatalf("tag = %x, want %x", actual, expected)
		}
	})

	t.Run("ProjectVectors", func(t *testing.T) {
		project := fixture.Project
		if project.Version != argon2.Version || project.Passes != 1 || project.Parallelism != 1 || project.TagLength != 32 {
			t.Fatalf("unexpected project parameters: %+v", project)
		}

		vectors := append([]argon2IDProjectVector{}, project.Vectors...)
		vectors = append(vectors, project.Transcripts...)
		for _, vector := range vectors {
			vector := vector
			t.Run(vector.Tag, func(t *testing.T) {
				nonce := project.Nonce
				body := project.PuzzleBody
				if vector.Nonce != "" {
					nonce = vector.Nonce
				}
				if vector.PuzzleBody != "" {
					body = vector.PuzzleBody
				}

				expectedTag := decodeArgon2IDHex(t, vector.Tag)
				if len(expectedTag) != int(project.TagLength) {
					t.Fatalf("tag length = %d, want %d", len(expectedTag), project.TagLength)
				}
				if value := binary.LittleEndian.Uint32(expectedTag); value != vector.Value {
					t.Fatalf("fixture value = %d, want little-endian tag value %d", vector.Value, value)
				}
				password := make([]byte, argon2IDPasswordSize)
				copy(password, decodeArgon2IDHex(t, body))
				copy(password[argon2IDPasswordSize-SolutionLength:], decodeArgon2IDHex(t, nonce))
				actualTag := argon2.IDKey(password, password[:16], project.Passes, vector.MemoryKiB, project.Parallelism, project.TagLength)
				if !bytes.Equal(actualTag, expectedTag) {
					t.Errorf("full tag = %x, want %x", actualTag, expectedTag)
				}

				actual, err := Argon2IDCandidateValue(
					decodeArgon2IDHex(t, nonce),
					decodeArgon2IDHex(t, body),
					vector.MemoryKiB,
				)
				if err != nil {
					t.Fatal(err)
				}
				if actual != vector.Value {
					var tag [4]byte
					binary.LittleEndian.PutUint32(tag[:], actual)
					t.Errorf("tag = %x, value = %d, want tag %s, value %d", tag, actual, vector.Tag, vector.Value)
				}
			})
		}
	})

	t.Run("InputValidation", func(t *testing.T) {
		project := fixture.Project
		nonce := decodeArgon2IDHex(t, project.Nonce)
		body := decodeArgon2IDHex(t, project.PuzzleBody)

		for _, tc := range []struct {
			name      string
			nonce     []byte
			body      []byte
			memoryKiB uint32
		}{
			{name: "ShortNonce", nonce: nonce[:len(nonce)-1], body: body, memoryKiB: argon2IDMemoryKiB},
			{name: "ShortBody", nonce: nonce, body: body[:len(body)-1], memoryKiB: argon2IDMemoryKiB},
			{name: "InsufficientMemory", nonce: nonce, body: body, memoryKiB: 8 * 1024},
			{name: "OtherMemory", nonce: nonce, body: body, memoryKiB: 32 * 1024},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := Argon2IDCandidateValue(tc.nonce, tc.body, tc.memoryKiB); !errors.Is(err, ErrInvalidArgon2IDInput) {
					t.Fatalf("error = %v, want %v", err, ErrInvalidArgon2IDInput)
				}
			})
		}
	})

	t.Run("Threshold", func(t *testing.T) {
		testCases := []struct {
			difficulty uint8
			threshold  uint32
		}{
			{difficulty: 0, threshold: 4294595181},
			{difficulty: 136, threshold: 32765},
			{difficulty: 152, threshold: 8191},
			{difficulty: 168, threshold: 2047},
			{difficulty: 255, threshold: 1},
		}
		for _, tc := range testCases {
			if actual := argon2IDThresholdFromDifficulty(tc.difficulty); actual != tc.threshold {
				t.Errorf("difficulty %d threshold = %d, want %d", tc.difficulty, actual, tc.threshold)
			}
		}

		if legacy := thresholdFromDifficulty(0); legacy != ^uint32(0) {
			t.Fatalf("legacy zero-difficulty threshold = %d, want %d", legacy, uint32(^uint32(0)))
		}
		if legacy := thresholdFromDifficulty(136); legacy != 32767 {
			t.Fatalf("legacy difficulty 136 threshold = %d, want 32767", legacy)
		}
	})

	t.Run("SolutionSet", func(t *testing.T) {
		project := fixture.Project
		nonce := decodeArgon2IDHex(t, project.Nonce)
		body := decodeArgon2IDHex(t, project.PuzzleBody)
		solutions := &Solutions{Buffer: nonce}

		valid, err := solutions.VerifyArgon2ID(t.Context(), body, 0, project.Vectors[0].MemoryKiB)
		if err != nil {
			t.Fatal(err)
		}
		if valid != 1 {
			t.Fatalf("valid solutions = %d, want 1", valid)
		}

		valid, err = solutions.VerifyArgon2ID(t.Context(), body, 255, project.Vectors[0].MemoryKiB)
		if err != nil {
			t.Fatal(err)
		}
		if valid != 0 {
			t.Fatalf("valid solutions = %d, want 0", valid)
		}

		cancelled, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := solutions.VerifyArgon2ID(cancelled, body, 0, project.Vectors[0].MemoryKiB); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v, want %v", err, context.Canceled)
		}

		if _, err := (&Solutions{Buffer: nonce[:len(nonce)-1]}).VerifyArgon2ID(t.Context(), body, 0, project.Vectors[0].MemoryKiB); !errors.Is(err, ErrInvalidArgon2IDInput) {
			t.Fatalf("error = %v, want %v", err, ErrInvalidArgon2IDInput)
		}
	})
}

func TestArgon2IDWireDifficulty(t *testing.T) {
	for _, tc := range []struct {
		logical  uint8
		wire     uint8
		useArgon bool
	}{
		{logical: 0},
		{logical: 127},
		{logical: 128},
		{logical: 136},
		{logical: 151},
		{logical: 152, wire: 24, useArgon: true},
		{logical: 168, wire: 40, useArgon: true},
		{logical: 255, wire: 127, useArgon: true},
	} {
		wire, useArgon := Argon2IDWireDifficulty(tc.logical)
		if wire != tc.wire || useArgon != tc.useArgon {
			t.Errorf("logical difficulty %d: wire = %d, useArgon = %t; want %d, %t", tc.logical, wire, useArgon, tc.wire, tc.useArgon)
		}
	}
}
