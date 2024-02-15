package puzzle

import (
	"context"
	"fmt"
	"testing"
)

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

	for i := 0; i < times; i++ {
		t.Run(fmt.Sprintf("solver_%v", i), func(t *testing.T) {
			t.Parallel()

			p, err := NewPuzzle()
			if err != nil {
				t.Fatal(err)
			}

			p.Difficulty = difficulty

			solver := &Solver{}
			solutions, err := solver.Solve(p)
			if err != nil {
				t.Fatal(err)
			}

			if err := solutions.CheckUnique(context.TODO()); err != nil {
				t.Fatal(err)
			}

			puzzleBytes, _ := p.MarshalBinary()
			puzzleBytes = normalizePuzzleBuffer(puzzleBytes)
			found, err := solutions.Verify(context.TODO(), puzzleBytes, difficulty)
			if err != nil {
				t.Fatal(err)
			}

			if found != int(p.SolutionsCount) {
				t.Errorf("Found %v solutions, but expected %v", found, p.SolutionsCount)
			}
		})
	}
}

func benchmarkDifficulty(difficulty uint8, b *testing.B) {
	for n := 0; n < b.N; n++ {
		p, err := NewPuzzle()
		if err != nil {
			b.Fatal(err)
		}
		p.Difficulty = difficulty

		solver := &Solver{}
		solutions, err := solver.Solve(p)
		if err != nil {
			b.Fatal(err)
		}

		puzzleBytes, _ := p.MarshalBinary()
		puzzleBytes = normalizePuzzleBuffer(puzzleBytes)
		_, err = solutions.Verify(context.TODO(), puzzleBytes, difficulty)
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
