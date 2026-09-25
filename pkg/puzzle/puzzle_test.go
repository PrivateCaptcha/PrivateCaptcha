package puzzle

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"math/rand"
	"testing"
	"time"
)

type protocolFixture struct {
	Version        uint8
	Challenge      uint8
	PropertyID     string
	PuzzleID       uint64
	Difficulty     uint8
	SolutionsCount uint8
	ExpirationUnix uint32
	UserData       string
	Body           string
}

type protocolFixtures struct {
	V1 protocolFixture
	V2 protocolFixture
}

func loadProtocolFixtures(t *testing.T) protocolFixtures {
	t.Helper()
	const (
		propertyID = "000102030405060708090a0b0c0d0e0f"
		userData   = "101112131415161718191a1b1c1d1e1f"
	)
	return protocolFixtures{
		V1: protocolFixture{
			Version: 1, Challenge: 0, PropertyID: propertyID, PuzzleID: 72623859790382856,
			Difficulty: 152, SolutionsCount: 24, ExpirationUnix: 1735689600, UserData: userData,
			Body: "01000102030405060708090a0b0c0d0e0f0807060504030201981880857467101112131415161718191a1b1c1d1e1f",
		},
		V2: protocolFixture{
			Version: 2, Challenge: 1, PropertyID: propertyID, PuzzleID: 72623859790382856,
			Difficulty: 152, SolutionsCount: 24, ExpirationUnix: 1735689600, UserData: userData,
			Body: "0201000102030405060708090a0b0c0d0e0f0807060504030201981880857467101112131415161718191a1b1c1d1e1f",
		},
	}
}

func decodeFixtureHex(t *testing.T, value, field string, size int) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != size {
		t.Fatalf("%s length = %d, want %d", field, len(decoded), size)
	}
	return decoded
}

func puzzleFromFixture(t *testing.T, fixture protocolFixture) *ComputePuzzle {
	t.Helper()
	propertyID := decodeFixtureHex(t, fixture.PropertyID, "property ID", PropertyIDSize)
	userData := decodeFixtureHex(t, fixture.UserData, "user data", UserDataSize)

	var propertyIDArray [PropertyIDSize]byte
	copy(propertyIDArray[:], propertyID)
	p, err := NewComputePuzzleForChallenge(fixture.PuzzleID, propertyIDArray, fixture.Difficulty, Challenge(fixture.Challenge))
	if err != nil {
		t.Fatal(err)
	}
	if p.version != fixture.Version {
		t.Fatalf("version = %d, want %d", p.version, fixture.Version)
	}
	p.solutionsCount = fixture.SolutionsCount
	p.expiration = time.Unix(int64(fixture.ExpirationUnix), 0).UTC()
	p.userData = userData
	return p
}

func fixtureBody(t *testing.T, fixture protocolFixture) []byte {
	t.Helper()
	body, err := hex.DecodeString(fixture.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestComputePuzzleV1GoldenBytes(t *testing.T) {
	t.Parallel()
	fixture := loadProtocolFixtures(t).V1
	p := puzzleFromFixture(t, fixture)

	actual, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	expected := fixtureBody(t, fixture)
	if len(expected) != 47 {
		t.Fatalf("golden body length = %d, want 47", len(expected))
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("v1 body = %x, want %x", actual, expected)
	}
}

func TestComputePuzzleV1ImplicitlyUsesBlake2b(t *testing.T) {
	t.Parallel()
	fixture := loadProtocolFixtures(t).V1

	var p ComputePuzzle
	if err := p.UnmarshalBinary(fixtureBody(t, fixture)); err != nil {
		t.Fatal(err)
	}
	if p.Challenge() != ChallengeBlake2b {
		t.Fatalf("challenge = %d, want %d", p.Challenge(), ChallengeBlake2b)
	}
}

func TestComputePuzzleV2GoldenBytes(t *testing.T) {
	t.Parallel()
	fixture := loadProtocolFixtures(t).V2
	p := puzzleFromFixture(t, fixture)

	actual, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	expected := fixtureBody(t, fixture)
	if len(expected) != 48 {
		t.Fatalf("golden body length = %d, want 48", len(expected))
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("v2 body = %x, want %x", actual, expected)
	}

	var decoded ComputePuzzle
	if err := decoded.UnmarshalBinary(expected); err != nil {
		t.Fatal(err)
	}
	checkPuzzles(p, &decoded, t)
}

func TestComputePuzzleRejectsInvalidWireValues(t *testing.T) {
	t.Parallel()
	fixtures := loadProtocolFixtures(t)
	v1 := fixtureBody(t, fixtures.V1)
	v2 := fixtureBody(t, fixtures.V2)

	unknownVersion := bytes.Clone(v1)
	unknownVersion[0] = 3
	unknownChallenge := bytes.Clone(v2)
	unknownChallenge[1] = 255

	tests := []struct {
		name string
		body []byte
	}{
		{"UnknownVersion", unknownVersion},
		{"UnknownChallenge", unknownChallenge},
		{"ShortV1", v1[:len(v1)-1]},
		{"TrailingV1", append(bytes.Clone(v1), 0)},
		{"ShortV2", v2[:len(v2)-1]},
		{"TrailingV2", append(bytes.Clone(v2), 0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p ComputePuzzle
			if err := p.UnmarshalBinary(tt.body); err == nil {
				t.Fatal("expected invalid puzzle body to fail")
			}
		})
	}
}

// limitedWriter is an io.Writer that returns an error after writing N bytes
type limitedWriter struct {
	limit   int
	written int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.written >= w.limit {
		return 0, io.ErrShortWrite
	}
	remaining := w.limit - w.written
	if len(p) <= remaining {
		w.written += len(p)
		return len(p), nil
	}
	w.written += remaining
	return remaining, io.ErrShortWrite
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
	if oldPuzzle.Challenge() != newPuzzle.Challenge() {
		t.Errorf("Challenge does not match")
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
	puzzle := NewComputePuzzle(0, [PropertyIDSize]byte{}, 0)

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
	solutions, err := solver.Solve(context.Background(), p)
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
		{"7", DefaultValidityPeriod},
		{"8", DefaultValidityPeriod},
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
		{2 * 24 * time.Hour, 3},
		{7 * 24 * time.Hour, 3},
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

func TestComputePuzzleCapsValidity(t *testing.T) {
	t.Parallel()

	p := NewComputePuzzle(NextPuzzleID(), [PropertyIDSize]byte{}, 1)
	if err := p.Init(48 * time.Hour); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()

	if expiration := p.Expiration(); expiration.After(after.Truncate(time.Second).Add(MaxValidityPeriod)) {
		t.Errorf("expiration = %v, want at most %v after now", expiration, MaxValidityPeriod)
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
