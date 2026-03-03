package puzzle

import (
	"encoding/base64"
	"encoding/binary"
	randv2 "math/rand/v2"
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

func TestMetadataMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		metadata      *Metadata
		wantWasmFlag  bool
		wantErrorCode uint8
		wantElapsed   uint32
	}{
		{
			name:          "NilMetadata",
			metadata:      nil,
			wantWasmFlag:  false,
			wantErrorCode: 0,
			wantElapsed:   0,
		},
		{
			name:          "WasmFlagTrue",
			metadata:      &Metadata{wasmFlag: true, errorCode: 0, elapsedMillis: 0},
			wantWasmFlag:  true,
			wantErrorCode: 0,
			wantElapsed:   0,
		},
		{
			name:          "AllFieldsSet",
			metadata:      &Metadata{wasmFlag: true, errorCode: 42, elapsedMillis: 12345},
			wantWasmFlag:  true,
			wantErrorCode: 42,
			wantElapsed:   12345,
		},
		{
			name:          "WasmFlagFalse",
			metadata:      &Metadata{wasmFlag: false, errorCode: 0, elapsedMillis: 0},
			wantWasmFlag:  false,
			wantErrorCode: 0,
			wantElapsed:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metadata.WasmFlag(); got != tt.wantWasmFlag {
				t.Errorf("WasmFlag() = %v, want %v", got, tt.wantWasmFlag)
			}
			if got := tt.metadata.ErrorCode(); got != tt.wantErrorCode {
				t.Errorf("ErrorCode() = %v, want %v", got, tt.wantErrorCode)
			}
			if got := tt.metadata.ElapsedMillis(); got != tt.wantElapsed {
				t.Errorf("ElapsedMillis() = %v, want %v", got, tt.wantElapsed)
			}
		})
	}
}

func TestNewSolutionsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        []byte
		expectError bool
		errorMsg    string
	}{
		{
			name:        "EmptyData",
			data:        []byte{},
			expectError: true,
			errorMsg:    "empty encoded solutions",
		},
		{
			name:        "NilData",
			data:        nil,
			expectError: true,
			errorMsg:    "empty encoded solutions",
		},
		{
			name:        "InvalidBase64",
			data:        []byte("not-valid-base64!!!"),
			expectError: true,
			errorMsg:    "invalid base64",
		},
		{
			name:        "InvalidVersion",
			data:        []byte(base64.StdEncoding.EncodeToString([]byte{2, 0, 0, 0, 0, 0, 0})), // version 2 is invalid
			expectError: true,
			errorMsg:    "invalid version",
		},
		{
			name:        "InvalidSolutionLength",
			data:        []byte(base64.StdEncoding.EncodeToString([]byte{1, 0, 0, 0, 0, 0, 0, 1, 2, 3})), // 3 bytes is not multiple of 8
			expectError: true,
			errorMsg:    "not SolutionLength multiple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSolutions(tt.data)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestNewSolutionsValidData(t *testing.T) {
	t.Parallel()

	// Create valid metadata + solutions data
	metadata := &Metadata{
		errorCode:     0,
		wasmFlag:      true,
		elapsedMillis: 1000,
	}

	metadataBytes, err := metadata.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	// Add one valid solution (8 bytes)
	solutionBytes := make([]byte, SolutionLength)
	for i := range solutionBytes {
		solutionBytes[i] = byte(i)
	}

	fullData := append(metadataBytes, solutionBytes...)
	encodedData := []byte(base64.StdEncoding.EncodeToString(fullData))

	solutions, err := NewSolutions(encodedData)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if solutions == nil {
		t.Fatal("Expected solutions to be non-nil")
	}

	if !solutions.Metadata.WasmFlag() {
		t.Error("Expected WasmFlag to be true")
	}

	if solutions.Metadata.ElapsedMillis() != 1000 {
		t.Errorf("Expected ElapsedMillis to be 1000, got %d", solutions.Metadata.ElapsedMillis())
	}

	if len(solutions.Buffer) != SolutionLength {
		t.Errorf("Expected buffer length %d, got %d", SolutionLength, len(solutions.Buffer))
	}
}

func TestMetadataUnmarshalBinaryErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        []byte
		expectError bool
	}{
		{
			name:        "TooShort",
			data:        []byte{1, 2, 3},
			expectError: true,
		},
		{
			name:        "InvalidVersion",
			data:        []byte{2, 0, 0, 0, 0, 0, 0},
			expectError: true,
		},
		{
			name:        "ValidData",
			data:        []byte{1, 0, 1, 0, 0, 0, 0}, // version=1, errorCode=0, wasmFlag=1, elapsedMillis=0
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Metadata{}
			err := m.UnmarshalBinary(tt.data)
			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

func TestSolutionsString(t *testing.T) {
	t.Parallel()

	solutions := emptySolutions(1)
	str := solutions.String()

	if len(str) == 0 {
		t.Error("Expected non-empty string")
	}

	// Verify it's valid base64
	_, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		t.Errorf("Expected valid base64 string, got error: %v", err)
	}
}

func TestSolutionsVerifyInvalidPuzzleBytes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	solutions := &Solutions{Buffer: make([]byte, SolutionLength)}

	// Test with invalid puzzle bytes length
	_, err := solutions.Verify(ctx, make([]byte, 10), 100)
	if err != ErrInvalidPuzzleBytes {
		t.Errorf("Expected ErrInvalidPuzzleBytes, got: %v", err)
	}
}

func setupSolutionsBenchmark() *Solutions {
	const (
		solutionsCount = 24
	)
	// Create a buffer with 16 unique uint64 values
	buf := make([]byte, solutionsCount*SolutionLength)
	for i := 0; i < solutionsCount; i++ {
		binary.LittleEndian.PutUint64(buf[i*SolutionLength:], randv2.Uint64())
	}
	return &Solutions{Buffer: buf}
}

func BenchmarkCheckUniqueMap(b *testing.B) {
	s := setupSolutionsBenchmark()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.checkUniqueMap()
	}
}

func BenchmarkCheckUniqueArray(b *testing.B) {
	s := setupSolutionsBenchmark()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.checkUniqueArray()
	}
}
