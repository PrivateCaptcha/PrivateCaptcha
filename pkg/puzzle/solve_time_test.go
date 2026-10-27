package puzzle

import (
	"testing"
	"time"
)

func TestVerifySolveTimeSkipsArgon2ID(t *testing.T) {
	for _, difficulty := range []uint8{0, 24, 168, 255} {
		p, err := NewComputePuzzleForChallenge(123, [PropertyIDSize]byte{}, difficulty, ChallengeArgon2ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, elapsed := range []uint32{0, 1, 99} {
			metadata := &Metadata{
				elapsedMillis: elapsed,
			}
			if result := verifySolveTime(p, metadata, new(time.Hour)); result != VerifyNoError {
				t.Fatalf("Argon2id difficulty %d, elapsed %dms: got %v, want %v", difficulty, elapsed, result, VerifyNoError)
			}
		}
	}
}

func TestVerifySolveTimeBenchmarksOnlyGateEligibility(t *testing.T) {
	const bucket = len(blake2bSolveTimeSeconds) - 1
	previous := blake2bSolveTimeSeconds[bucket]
	t.Cleanup(func() { blake2bSolveTimeSeconds[bucket] = previous })
	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 200)
	for _, seconds := range []float64{1, 1000} {
		blake2bSolveTimeSeconds[bucket] = seconds
		metadata := &Metadata{
			elapsedMillis: 1599,
		}
		if result := verifySolveTime(p, metadata, new(100*time.Millisecond)); result != SolutionTooFastError {
			t.Fatalf("benchmark %vs, elapsed 1599ms: got %v, want %v", seconds, result, SolutionTooFastError)
		}
		metadata.elapsedMillis = 1600
		if result := verifySolveTime(p, metadata, new(100*time.Millisecond)); result != VerifyNoError {
			t.Fatalf("benchmark %vs, elapsed 1600ms: got %v, want %v", seconds, result, VerifyNoError)
		}
	}
}
