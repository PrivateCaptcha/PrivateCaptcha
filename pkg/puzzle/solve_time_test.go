package puzzle

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestVerifySolveTimePuzzleAge(t *testing.T) {
	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 168)
	for _, tc := range []struct {
		name    string
		elapsed uint32
		age     *time.Duration
		want    VerifyError
	}{
		{
			name:    "WithinAge",
			elapsed: 999,
			age:     new(time.Second),
			want:    VerifyNoError,
		},
		{
			name:    "EqualAge",
			elapsed: 1000,
			age:     new(time.Second),
			want:    VerifyNoError,
		},
		{
			name:    "OverAge",
			elapsed: 1001,
			age:     new(time.Second),
			want:    SolutionTimingError,
		},
		{
			name:    "AgeAndWidgetBelowMinimum",
			elapsed: 50,
			age:     new(50 * time.Millisecond),
			want:    SolutionTimingError,
		},
		{
			name:    "WidgetMeetsMinimumButPuzzleTooYoung",
			elapsed: 100,
			age:     new(50 * time.Millisecond),
			want:    SolutionTimingError,
		},
		{
			name:    "WidgetBelowMinimum",
			elapsed: 99,
			age:     new(time.Second),
			want:    SolutionTimingError,
		},
		{
			name:    "UnknownAge",
			elapsed: 1000,
			age:     nil,
			want:    VerifyNoError,
		},
		{
			name:    "NegativeMetadata",
			elapsed: ^uint32(0),
			age:     new(time.Millisecond),
			want:    SolutionTimingError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := &Metadata{
				elapsedMillis: tc.elapsed,
			}
			if result := verifySolveTime(t.Context(), p, metadata, new(100*time.Millisecond), tc.age); result != tc.want {
				t.Fatalf("elapsed %dms, age %v: got %v, want %v", tc.elapsed, tc.age, result, tc.want)
			}
		})
	}
}

func TestVerifySolveTimeLogsImpossibleElapsed(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	p := NewComputePuzzle(123, [PropertyIDSize]byte{}, 168)
	metadata := &Metadata{
		elapsedMillis: 1001,
	}
	if result := verifySolveTime(t.Context(), p, metadata, new(100*time.Millisecond), new(time.Second)); result != SolutionTimingError {
		t.Fatalf("impossible elapsed: got %v, want %v", result, SolutionTimingError)
	}
	for _, expected := range []string{"Reported solve time exceeds puzzle age", "elapsedMillis=1001", "puzzleAgeMillis=1000"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("log %q does not contain %q", logs.String(), expected)
		}
	}
	logs.Reset()
	metadata.elapsedMillis = 1000
	if result := verifySolveTime(t.Context(), p, metadata, new(100*time.Millisecond), new(time.Second)); result != VerifyNoError || logs.Len() != 0 {
		t.Fatalf("equal age: got %v, log = %q", result, logs.String())
	}
}

func TestVerifySolveTimeSkipsArgon2IDMinimum(t *testing.T) {
	for _, difficulty := range []uint8{0, 24, 168, 255} {
		p, err := NewComputePuzzleForChallenge(123, [PropertyIDSize]byte{}, difficulty, ChallengeArgon2ID)
		if err != nil {
			t.Fatal(err)
		}
		for elapsed, want := range map[uint32]VerifyError{
			0:          VerifyNoError,
			1:          VerifyNoError,
			99:         SolutionTimingError,
			^uint32(0): SolutionTimingError,
		} {
			metadata := &Metadata{
				elapsedMillis: elapsed,
			}
			for _, minimum := range []*time.Duration{nil, new(time.Hour)} {
				if result := verifySolveTime(t.Context(), p, metadata, minimum, new(time.Millisecond)); result != want {
					t.Fatalf("Argon2id difficulty %d, elapsed %dms: got %v, want %v", difficulty, elapsed, result, want)
				}
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
		if result := verifySolveTime(t.Context(), p, metadata, new(100*time.Millisecond), nil); result != SolutionTimingError {
			t.Fatalf("benchmark %vs, elapsed 1599ms: got %v, want %v", seconds, result, SolutionTimingError)
		}
		metadata.elapsedMillis = 1600
		if result := verifySolveTime(t.Context(), p, metadata, new(100*time.Millisecond), nil); result != VerifyNoError {
			t.Fatalf("benchmark %vs, elapsed 1600ms: got %v, want %v", seconds, result, VerifyNoError)
		}
	}
}
