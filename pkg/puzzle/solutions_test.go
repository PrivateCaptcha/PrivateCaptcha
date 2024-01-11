package puzzle

import (
	"context"
	"testing"
)

func TestUniqueSolutions(t *testing.T) {
	solution := make([]byte, SolutionLength)
	for i := 0; i < SolutionLength; i++ {
		solution[i] = byte(i)
	}

	ctx := context.TODO()

	solutions := &Solutions{Buffer: solution}
	if err := solutions.CheckUnique(ctx); err != nil {
		t.Fatal(err)
	}

	buffer := make([]byte, SolutionLength*2)
	copy(buffer, solution)
	copy(buffer[SolutionLength:], solution)

	solutions = &Solutions{Buffer: buffer}
	if err := solutions.CheckUnique(ctx); err == nil {
		t.Error("Duplicate was not detected")
	}
}
