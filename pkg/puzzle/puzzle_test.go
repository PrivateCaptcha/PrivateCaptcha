package puzzle

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
)

func randInit(data []byte) {
	for i := range data {
		data[i] = byte(rand.Intn(256))
	}
}

func TestNewPuzzleIsZero(t *testing.T) {
	t.Parallel()

	if !new(Puzzle).IsZero() {
		t.Error("new puzzle is not zero!")
	}
}

func TestPuzzleUnmarshalFail(t *testing.T) {
	t.Parallel()

	puzzle := NewPuzzle(RandomPuzzleID(), [16]byte{}, 123)

	randInit(puzzle.PropertyID[:])

	data, err := puzzle.MarshalBinary()
	if err != nil {
		t.Fatalf("Error marshalling: %v", err)
	}

	var newPuzzle Puzzle
	if err := newPuzzle.UnmarshalBinary(data[:len(data)-1]); err != io.ErrShortBuffer {
		t.Error("Buffer is not too short")
	}
}

func checkPuzzles(oldPuzzle, newPuzzle *Puzzle, t *testing.T) {
	if !bytes.Equal(oldPuzzle.PropertyID[:], newPuzzle.PropertyID[:]) {
		t.Errorf("PropertyID does not match")
	}

	if oldPuzzle.PuzzleID != newPuzzle.PuzzleID {
		t.Errorf("PuzzleID does not match")
	}

	if oldPuzzle.Expiration.Unix() != newPuzzle.Expiration.Unix() {
		t.Errorf("Expiration does not match: old (%v), new (%v)", oldPuzzle.Expiration, newPuzzle.Expiration)
	}

	if oldPuzzle.Difficulty != newPuzzle.Difficulty {
		t.Errorf("Difficulty does not match")
	}

	if oldPuzzle.SolutionsCount != newPuzzle.SolutionsCount {
		t.Errorf("SolutionsCount does not match")
	}

	if oldPuzzle.Version != newPuzzle.Version {
		t.Errorf("Version does not match")
	}

	if oldPuzzle.IsStub() != newPuzzle.IsStub() {
		t.Errorf("Stub flag does not match")
	}

	if !bytes.Equal(oldPuzzle.UserData, newPuzzle.UserData) {
		t.Errorf("UserData does not match")
	}
}

func TestPuzzleMarshalling(t *testing.T) {
	t.Parallel()
	propertyID := [16]byte{}
	randInit(propertyID[:])

	// Create a sample Puzzle
	puzzle := NewPuzzle(RandomPuzzleID(), propertyID, 123)
	_ = puzzle.Init(DefaultValidityPeriod)

	// Marshal the Puzzle to a byte slice
	data, err := puzzle.MarshalBinary()
	if err != nil {
		t.Fatalf("Error marshalling: %v", err)
	}

	// Unmarshal the byte slice into a new Puzzle
	var newPuzzle Puzzle
	if err := newPuzzle.UnmarshalBinary(data); err != nil {
		t.Fatalf("Error unmarshalling: %v", err)
	}

	checkPuzzles(puzzle, &newPuzzle, t)
}

func TestZeroPuzzleMarshalling(t *testing.T) {
	t.Parallel()
	// Create a sample Puzzle
	puzzle := new(Puzzle)
	puzzle.UserData = make([]byte, UserDataSize)

	//puzzle.Init(propertyID, 123)

	// Marshal the Puzzle to a byte slice
	data, err := puzzle.MarshalBinary()
	if err != nil {
		t.Fatalf("Error marshalling: %v", err)
	}

	// Unmarshal the byte slice into a new Puzzle
	var newPuzzle Puzzle
	if err := newPuzzle.UnmarshalBinary(data); err != nil {
		t.Fatalf("Error unmarshalling: %v", err)
	}

	checkPuzzles(puzzle, &newPuzzle, t)
}
