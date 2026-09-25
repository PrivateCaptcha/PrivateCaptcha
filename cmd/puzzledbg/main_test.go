package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

func TestPuzzledbg(t *testing.T) {
	for _, tc := range []struct {
		name      string
		challenge puzzle.Challenge
		version   float64
		label     string
	}{
		{name: "Blake", challenge: puzzle.ChallengeBlake2b, version: 1, label: "blake2b"},
		{name: "Argon", challenge: puzzle.ChallengeArgon2ID, version: 2, label: "argon2id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := puzzle.NewComputePuzzleForChallenge(123, [puzzle.PropertyIDSize]byte{1}, 0, tc.challenge)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Init(puzzle.DefaultValidityPeriod); err != nil {
				t.Fatal(err)
			}
			salt := puzzle.NewSalt([]byte("test-salt"))
			signed, err := p.Serialize(t.Context(), salt, nil)
			if err != nil {
				t.Fatal(err)
			}
			var encoded bytes.Buffer
			if err := signed.Write(&encoded); err != nil {
				t.Fatal(err)
			}
			info, err := puzzleOutput(t.Context(), p, encoded.String(), false)
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(info), &data); err != nil {
				t.Fatal(err)
			}
			if data["Version"] != tc.version || data["Challenge"] != tc.label {
				t.Fatalf("version/challenge = (%v, %v), want (%v, %s)", data["Version"], data["Challenge"], tc.version, tc.label)
			}
			solved, err := puzzleOutput(t.Context(), p, encoded.String(), true)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := puzzle.ParseVerifyPayload[puzzle.ComputePuzzle](t.Context(), []byte(solved))
			if err != nil {
				t.Fatal(err)
			}
			if err := payload.VerifySignature(t.Context(), salt, nil); err != nil {
				t.Fatal(err)
			}
			if _, result := payload.VerifySolutions(t.Context()); result != puzzle.VerifyNoError {
				t.Fatalf("solved payload verification = %s", result)
			}
		})
	}
}
