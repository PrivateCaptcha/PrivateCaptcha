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

	if !new(ComputePuzzle).IsZero() {
		t.Error("new puzzle is not zero!")
	}
}

func TestPuzzleUnmarshalFail(t *testing.T) {
	t.Parallel()

	puzzle := NewComputePuzzle(NextPuzzleID(), [16]byte{}, 123)

	randInit(puzzle.propertyID[:])

	data, err := puzzle.MarshalBinary()
	if err != nil {
		t.Fatalf("Error marshalling: %v", err)
	}

	var newPuzzle ComputePuzzle
	if err := newPuzzle.UnmarshalBinary(data[:len(data)-1]); err != io.ErrShortBuffer {
		t.Error("Buffer is not too short")
	}
}

func checkPuzzles(oldPuzzle, newPuzzle *ComputePuzzle, t *testing.T) {
	if !bytes.Equal(oldPuzzle.propertyID[:], newPuzzle.propertyID[:]) {
		t.Errorf("PropertyID does not match")
	}

	if oldPuzzle.PuzzleID() != newPuzzle.PuzzleID() {
		t.Errorf("PuzzleID does not match")
	}

	if oldPuzzle.Expiration().Unix() != newPuzzle.Expiration().Unix() {
		t.Errorf("Expiration does not match: old (%v), new (%v)", oldPuzzle.Expiration(), newPuzzle.Expiration())
	}

	if oldPuzzle.Difficulty() != newPuzzle.Difficulty() {
		t.Errorf("Difficulty does not match")
	}

	if oldPuzzle.SolutionsCount() != newPuzzle.SolutionsCount() {
		t.Errorf("SolutionsCount does not match")
	}

	if oldPuzzle.version != newPuzzle.version {
		t.Errorf("Version does not match")
	}

	if oldPuzzle.IsStub() != newPuzzle.IsStub() {
		t.Errorf("Stub flag does not match")
	}

	if !bytes.Equal(oldPuzzle.userData, newPuzzle.userData) {
		t.Errorf("UserData does not match")
	}
}

func TestPuzzleMarshalling(t *testing.T) {
	t.Parallel()
	propertyID := [16]byte{}
	randInit(propertyID[:])

	// Create a sample Puzzle
	puzzle := NewComputePuzzle(NextPuzzleID(), propertyID, 123)
	_ = puzzle.Init(DefaultValidityPeriod)

	// Marshal the Puzzle to a byte slice
	data, err := puzzle.MarshalBinary()
	if err != nil {
		t.Fatalf("Error marshalling: %v", err)
	}

	// Unmarshal the byte slice into a new Puzzle
	var newPuzzle ComputePuzzle
	if err := newPuzzle.UnmarshalBinary(data); err != nil {
		t.Fatalf("Error unmarshalling: %v", err)
	}

	checkPuzzles(puzzle, &newPuzzle, t)
}

func TestZeroPuzzleMarshalling(t *testing.T) {
	t.Parallel()
	// Create a sample Puzzle
	puzzle := new(ComputePuzzle)
	puzzle.userData = make([]byte, UserDataSize)

	//puzzle.Init(propertyID, 123)

	// Marshal the Puzzle to a byte slice
	data, err := puzzle.MarshalBinary()
	if err != nil {
		t.Fatalf("Error marshalling: %v", err)
	}

	// Unmarshal the byte slice into a new Puzzle
	var newPuzzle ComputePuzzle
	if err := newPuzzle.UnmarshalBinary(data); err != nil {
		t.Fatalf("Error unmarshalling: %v", err)
	}

	checkPuzzles(puzzle, &newPuzzle, t)
}

func TestPuzzlePayloadSuffix(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	solution := make([]byte, SolutionLength)
	for i := 0; i < SolutionLength; i++ {
		solution[i] = byte(i)
	}

	propertyID := [16]byte{}
	randInit(propertyID[:])
	p := NewComputePuzzle(0 /*puzzle ID*/, propertyID, 0 /*difficulty*/)

	solver := &ComputeSolver{}
	solutions, err := solver.Solve(p)
	if err != nil {
		t.Fatal(err)
	}

	salt := NewSalt([]byte("salt"))
	puzzleData, err := p.Serialize(ctx, salt, nil /*property salt*/)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	buf.WriteString(solutions.String())
	buf.Write(dotBytes)
	puzzleData.Write(&buf)

	data := buf.Bytes()
	dlen := len(data)

	if puzzleData.IsSuffixFor(data[dlen-puzzleData.Size()+1:]) {
		t.Error("Is suffix for shorter bytes")
	}

	if !puzzleData.IsSuffixFor(data[dlen-puzzleData.Size():]) {
		t.Error("Not suffix for just enough bytes")
	}

	if !puzzleData.IsSuffixFor(data) {
		t.Error("Not suffix for full bytes")
	}

	puzzleRef := data[(dlen - puzzleData.Size()):]
	puzzleRef[len(puzzleData.puzzleBase64)-1]++
	if puzzleData.IsSuffixFor(data) {
		t.Error("Is suffix for modified puzzle")
	}
	puzzleRef[len(puzzleData.puzzleBase64)-1]--
	// ---------------------------------------------
	puzzleRef[len(puzzleData.puzzleBase64)]++
	if puzzleData.IsSuffixFor(data) {
		t.Error("Is suffix without dot")
	}
	puzzleRef[len(puzzleData.puzzleBase64)]--
	// ---------------------------------------------
	puzzleRef[len(puzzleData.puzzleBase64)+1]++
	if puzzleData.IsSuffixFor(data) {
		t.Error("Is suffix for modified signature")
	}
	puzzleRef[len(puzzleData.puzzleBase64)+1]--
}
