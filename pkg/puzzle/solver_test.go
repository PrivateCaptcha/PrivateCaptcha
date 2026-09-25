package puzzle

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestSolverArgon2ID(t *testing.T) {
	p, err := NewComputePuzzleForChallenge(123, [PropertyIDSize]byte{1}, 0, ChallengeArgon2ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Init(DefaultValidityPeriod); err != nil {
		t.Fatal(err)
	}
	if p.SolutionsCount() != Argon2IDSolutionsCount {
		t.Fatalf("solution count = %d, want %d", p.SolutionsCount(), Argon2IDSolutionsCount)
	}
	solutions, err := (&ComputeSolver{}).Solve(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(solutions.Buffer) != Argon2IDSolutionsCount*SolutionLength {
		t.Fatalf("solution bytes = %d", len(solutions.Buffer))
	}
	if err := solutions.CheckUnique(); err != nil {
		t.Fatal(err)
	}
	body, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for lane := range Argon2IDSolutionsCount {
		nonce := solutions.Buffer[lane*SolutionLength : (lane+1)*SolutionLength]
		if nonce[0] != byte(lane) || nonce[1] != 0 || nonce[2] != 0 || nonce[3] != 0 {
			t.Errorf("lane %d nonce = %x", lane, nonce)
		}
		value, err := Argon2IDCandidateValue(nonce, body, Argon2IDMemoryKiB)
		if err != nil || value > argon2IDThresholdFromDifficulty(p.Difficulty()) {
			t.Errorf("lane %d invalid Argon value %d: %v", lane, value, err)
		}
	}
}

func TestSolverArgon2IDExhaustion(t *testing.T) {
	body := make([]byte, puzzleV2Size)
	const threshold = uint32(0)
	hash := func([]byte, []byte, uint32) (uint32, error) { return 1, nil }
	if _, err := solveArgon2IDOne(t.Context(), body, 0, threshold, 1, hash); !errors.Is(err, ErrArgon2IDExhausted) {
		t.Fatalf("exhaustion error = %v, want %v", err, ErrArgon2IDExhausted)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := solveArgon2IDOne(ctx, body, 0, threshold, 1, hash); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v, want %v", err, context.Canceled)
	}
}

func TestDifficultyToThreshold(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		difficulty byte
		threshold  uint32
	}{
		{0, 0xffffffff},
		{255, 1},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("difficulty_%v", i), func(t *testing.T) {
			threshold := thresholdFromDifficulty(tc.difficulty)
			if threshold != tc.threshold {
				t.Errorf("Actual threshold (%v) is different from expected (%v)", threshold, tc.threshold)
			}
		})
	}
}

func TestSolver(t *testing.T) {
	times := 10
	difficulty := uint8(160)

	if testing.Short() {
		times = 1
		difficulty = 160
	}

	propertyID := [16]byte{}
	randInit(propertyID[:])

	for i := 0; i < times; i++ {
		t.Run(fmt.Sprintf("solver_%v", i), func(t *testing.T) {
			t.Parallel()

			p := NewComputePuzzle(NextPuzzleID(), propertyID, difficulty)
			if err := p.Init(DefaultValidityPeriod); err != nil {
				t.Fatal(err)
			}

			solver := &ComputeSolver{}
			solutions, err := solver.Solve(t.Context(), p)
			if err != nil {
				t.Fatal(err)
			}

			if err := solutions.CheckUnique(); err != nil {
				t.Fatal(err)
			}

			puzzleBytes, _ := p.MarshalBinary()
			puzzleBytes = normalizePuzzleBuffer(puzzleBytes)
			found, err := solutions.VerifyBlake2b(t.Context(), puzzleBytes, difficulty)
			if err != nil {
				t.Fatal(err)
			}

			if found != p.SolutionsCount() {
				t.Errorf("Found %v solutions, but expected %v", found, p.SolutionsCount())
			}
		})
	}
}

func TestSolverCancellation(t *testing.T) {
	t.Parallel()

	propertyID := [16]byte{}
	randInit(propertyID[:])

	p := NewComputePuzzle(NextPuzzleID(), propertyID, 160)
	if err := p.Init(DefaultValidityPeriod); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	solver := &ComputeSolver{}
	if _, err := solver.Solve(ctx, p); err == nil {
		t.Fatal("Solve was not cancelled")
	}
}

func TestSolverWorkerCounts(t *testing.T) {
	for _, count := range []uint8{0, 1, 3} {
		t.Run(fmt.Sprintf("count-%d", count), func(t *testing.T) {
			p := NewComputePuzzle(1, [16]byte{}, 100)
			p.solutionsCount = count
			solutions, err := (&ComputeSolver{}).Solve(t.Context(), p)
			if err != nil {
				t.Fatal(err)
			}
			if len(solutions.Buffer) != int(count)*SolutionLength {
				t.Fatalf("got %d solution bytes for %d solutions", len(solutions.Buffer), count)
			}
			puzzleBytes, err := p.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			found, err := solutions.VerifyBlake2b(t.Context(), normalizePuzzleBuffer(puzzleBytes), p.Difficulty())
			if err != nil || found != int(count) {
				t.Fatalf("verified %d of %d solutions: %v", found, count, err)
			}
		})
	}
}

func benchmarkDifficulty(difficulty uint8, b *testing.B) {
	for n := 0; n < b.N; n++ {
		p := NewComputePuzzle(0, [16]byte{}, difficulty)
		if err := p.Init(DefaultValidityPeriod); err != nil {
			b.Fatal(err)
		}

		solver := &ComputeSolver{}
		solutions, err := solver.Solve(b.Context(), p)
		if err != nil {
			b.Fatal(err)
		}

		puzzleBytes, _ := p.MarshalBinary()
		puzzleBytes = normalizePuzzleBuffer(puzzleBytes)
		_, err = solutions.VerifyBlake2b(b.Context(), puzzleBytes, difficulty)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDifficulty10(b *testing.B) {
	benchmarkDifficulty(10, b)
}

func BenchmarkDifficulty50(b *testing.B) {
	benchmarkDifficulty(50, b)
}

func BenchmarkDifficulty100(b *testing.B) {
	benchmarkDifficulty(100, b)
}

func BenchmarkDifficulty130(b *testing.B) {
	benchmarkDifficulty(130, b)
}

func BenchmarkDifficulty150(b *testing.B) {
	benchmarkDifficulty(150, b)
}

func BenchmarkDifficulty165(b *testing.B) {
	benchmarkDifficulty(165, b)
}

func BenchmarkDifficulty180(b *testing.B) {
	benchmarkDifficulty(180, b)
}

func BenchmarkSolverSearch(b *testing.B) {
	buf := make([]byte, PuzzleBytesLength)
	buf[0] = 1
	solver := &ComputeSolver{}
	threshold := thresholdFromDifficulty(130)
	var solution [SolutionLength]byte
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		if err := solver.solveOne(b.Context(), buf, threshold, solution[:]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSolverFull(b *testing.B) {
	p := NewComputePuzzle(0, [16]byte{}, 100)
	p.userData[0] = 1
	solver := &ComputeSolver{}
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		if _, err := solver.Solve(b.Context(), p); err != nil {
			b.Fatal(err)
		}
	}
}
