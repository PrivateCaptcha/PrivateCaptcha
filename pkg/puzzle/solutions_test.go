package puzzle

import (
	"testing"
	"time"
)

func TestUniqueSolutions(t *testing.T) {
	t.Parallel()

	solution := make([]byte, SolutionLength)
	for i := 0; i < SolutionLength; i++ {
		solution[i] = byte(i)
	}

	solutions := &Solutions{Buffer: solution}
	if err := solutions.CheckUnique(); err != nil {
		t.Fatal(err)
	}

	buffer := make([]byte, SolutionLength*2)
	copy(buffer, solution)
	copy(buffer[SolutionLength:], solution)

	solutions = &Solutions{Buffer: buffer}
	if err := solutions.CheckUnique(); err == nil {
		t.Error("Duplicate was not detected")
	}
}

func TestZeroDifficulty(t *testing.T) {
	t.Parallel()

	const difficulty = 160

	propertyID := [16]byte{}
	randInit(propertyID[:])

	puzzle := NewComputePuzzle(NextPuzzleID(), propertyID, difficulty)
	_ = puzzle.Init(1 * time.Hour)

	puzzleBytes, err := puzzle.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	puzzleBytes = normalizePuzzleBuffer(puzzleBytes)

	solution := make([]byte, puzzle.SolutionsCount()*SolutionLength)
	for i := 0; i < puzzle.SolutionsCount()*SolutionLength; i++ {
		solution[i] = byte(i)
	}

	solutions := &Solutions{Buffer: solution}

	ctx := t.Context()
	if count, _ := solutions.Verify(ctx, puzzleBytes, difficulty); count > 0 {
		t.Fatal("Should have failed with random solutions")
	}

	if count, _ := solutions.Verify(ctx, puzzleBytes, 0 /*difficulty*/); count != puzzle.SolutionsCount() {
		t.Errorf("Zero difficulty should suffice. Solutions count %v, expected %v", count, puzzle.SolutionsCount())
	}
}
