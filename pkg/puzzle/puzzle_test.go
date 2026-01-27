package puzzle

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"testing"
	"time"
)

// limitedWriter is an io.Writer that returns an error after writing N bytes
type limitedWriter struct {
	limit   int
	written int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.written >= w.limit {
		return 0, errors.New("write limit reached")
	}
	remaining := w.limit - w.written
	if len(p) <= remaining {
		w.written += len(p)
		return len(p), nil
	}
	w.written += remaining
	return remaining, errors.New("write limit reached")
}

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
	t.Helper()

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

func TestValidityIntervalFromIndex(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	tests := []struct {
		index    string
		expected time.Duration
	}{
		{"0", 5 * time.Minute},
		{"1", 10 * time.Minute},
		{"2", 30 * time.Minute},
		{"3", 1 * time.Hour},
		{"4", 6 * time.Hour},
		{"5", 12 * time.Hour},
		{"6", 24 * time.Hour},
		{"7", 2 * 24 * time.Hour},
		{"8", 7 * 24 * time.Hour},
		{"-1", DefaultValidityPeriod},
		{"99", DefaultValidityPeriod},
		{"invalid", DefaultValidityPeriod},
		{"", DefaultValidityPeriod},
	}

	for _, tt := range tests {
		t.Run(tt.index, func(t *testing.T) {
			result := ValidityIntervalFromIndex(ctx, tt.index)
			if result != tt.expected {
				t.Errorf("ValidityIntervalFromIndex(%s) = %v, want %v", tt.index, result, tt.expected)
			}
		})
	}
}

func TestValidityIntervalToIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		duration time.Duration
		expected int
	}{
		{5 * time.Minute, 0},
		{10 * time.Minute, 1},
		{30 * time.Minute, 2},
		{1 * time.Hour, 3},
		{6 * time.Hour, 4},
		{12 * time.Hour, 5},
		{24 * time.Hour, 6},
		{2 * 24 * time.Hour, 7},
		{7 * 24 * time.Hour, 8},
		{99 * time.Hour, 3},
	}

	for _, tt := range tests {
		t.Run(tt.duration.String(), func(t *testing.T) {
			result := ValidityIntervalToIndex(tt.duration)
			if result != tt.expected {
				t.Errorf("ValidityIntervalToIndex(%v) = %d, want %d", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestComputePuzzleWriteToErrors(t *testing.T) {
	t.Parallel()

	propertyID := [16]byte{}
	randInit(propertyID[:])

	puzzle := NewComputePuzzle(NextPuzzleID(), propertyID, 123)
	_ = puzzle.Init(DefaultValidityPeriod)

	// Test error at version write (byte 0)
	t.Run("ErrorAtVersion", func(t *testing.T) {
		w := &limitedWriter{limit: 0}
		_, err := puzzle.WriteTo(w)
		if err == nil {
			t.Error("Expected error at version write")
		}
	})

	// Test error at propertyID write (after 1 byte)
	t.Run("ErrorAtPropertyID", func(t *testing.T) {
		w := &limitedWriter{limit: 1}
		_, err := puzzle.WriteTo(w)
		if err == nil {
			t.Error("Expected error at propertyID write")
		}
	})

	// Test error at puzzleID write (after 1 + 16 = 17 bytes)
	t.Run("ErrorAtPuzzleID", func(t *testing.T) {
		w := &limitedWriter{limit: 17}
		_, err := puzzle.WriteTo(w)
		if err == nil {
			t.Error("Expected error at puzzleID write")
		}
	})

	// Test error at difficulty write (after 1 + 16 + 8 = 25 bytes)
	t.Run("ErrorAtDifficulty", func(t *testing.T) {
		w := &limitedWriter{limit: 25}
		_, err := puzzle.WriteTo(w)
		if err == nil {
			t.Error("Expected error at difficulty write")
		}
	})

	// Test error at solutionsCount write (after 1 + 16 + 8 + 1 = 26 bytes)
	t.Run("ErrorAtSolutionsCount", func(t *testing.T) {
		w := &limitedWriter{limit: 26}
		_, err := puzzle.WriteTo(w)
		if err == nil {
			t.Error("Expected error at solutionsCount write")
		}
	})

	// Test error at expiration write (after 1 + 16 + 8 + 1 + 1 = 27 bytes)
	t.Run("ErrorAtExpiration", func(t *testing.T) {
		w := &limitedWriter{limit: 27}
		_, err := puzzle.WriteTo(w)
		if err == nil {
			t.Error("Expected error at expiration write")
		}
	})

	// Test error at userData write (after 1 + 16 + 8 + 1 + 1 + 4 = 31 bytes)
	t.Run("ErrorAtUserData", func(t *testing.T) {
		w := &limitedWriter{limit: 31}
		_, err := puzzle.WriteTo(w)
		if err == nil {
			t.Error("Expected error at userData write")
		}
	})
}

func TestPuzzlePayloadWriteErrors(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	propertyID := [16]byte{}
	randInit(propertyID[:])
	p := NewComputePuzzle(NextPuzzleID(), propertyID, 0 /*difficulty*/)
	_ = p.Init(DefaultValidityPeriod)

	salt := NewSalt([]byte("salt"))
	puzzleData, err := p.Serialize(ctx, salt, nil /*property salt*/)
	if err != nil {
		t.Fatal(err)
	}

	// Test error at puzzleBase64 write
	t.Run("ErrorAtPuzzleBase64", func(t *testing.T) {
		w := &limitedWriter{limit: 0}
		err := puzzleData.Write(w)
		if err == nil {
			t.Error("Expected error at puzzleBase64 write")
		}
	})

	// Test error at signatureBase64 write (after puzzleBase64 + dotBytes)
	t.Run("ErrorAtSignatureBase64", func(t *testing.T) {
		w := &limitedWriter{limit: len(puzzleData.puzzleBase64) + len(dotBytes)}
		err := puzzleData.Write(w)
		if err == nil {
			t.Error("Expected error at signatureBase64 write")
		}
	})

	// Test successful write
	t.Run("SuccessfulWrite", func(t *testing.T) {
		var buf bytes.Buffer
		err := puzzleData.Write(&buf)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if buf.Len() != puzzleData.Size() {
			t.Errorf("Expected size %d, got %d", puzzleData.Size(), buf.Len())
		}
	})
}
