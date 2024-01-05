package common

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

func TestPuzzleUnmarshalFail(t *testing.T) {
	puzzle, _ := NewPuzzle()

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

func TestPuzzleMarshalling(t *testing.T) {
	// Create a sample Puzzle
	puzzle, _ := NewPuzzle()

	randInit(puzzle.PropertyID[:])

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

	if !bytes.Equal(puzzle.PropertyID[:], newPuzzle.PropertyID[:]) {
		t.Errorf("PropertyID does not match")
	}

	if !bytes.Equal(puzzle.Nonce[:], newPuzzle.Nonce[:]) {
		t.Errorf("Nonce does not match")
	}

	if puzzle.Expiration.Unix() != newPuzzle.Expiration.Unix() {
		t.Errorf("Expiration does not match")
	}

	if puzzle.Difficulty != newPuzzle.Difficulty {
		t.Errorf("Difficulty does not match")
	}

	if puzzle.SolutionsCount != newPuzzle.SolutionsCount {
		t.Errorf("SolutionsCount does not match")
	}

	if puzzle.Version != newPuzzle.Version {
		t.Errorf("Version does not match")
	}

	if !bytes.Equal(puzzle.UserData, newPuzzle.UserData) {
		t.Errorf("UserData does not match")
	}
}
