package common

import (
	"bytes"
	"testing"
	"time"
)

func TestPuzzleMarshalling(t *testing.T) {
	// Create a sample Puzzle
	puzzle := Puzzle{
		AccountID:      123,
		PropertyID:     456,
		Nonce:          []byte("example nonce 16"),
		UserData:       []byte("userdata is 16 !"),
		Expiration:     time.Now(),
		Difficulty:     3,
		SolutionsCount: 5,
		Version:        1,
	}

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

	if puzzle.AccountID != newPuzzle.AccountID {
		t.Errorf("AccountID does not match")
	}

	if puzzle.PropertyID != newPuzzle.PropertyID {
		t.Errorf("PropertyID does not match")
	}

	if !bytes.Equal(puzzle.Nonce, newPuzzle.Nonce) {
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
