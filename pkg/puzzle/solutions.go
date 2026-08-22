package puzzle

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"golang.org/x/crypto/blake2b"
)

const (
	PuzzleBytesLength = 128
	SolutionLength    = 8
	metadataVersion   = 1
	metadataLength    = 1 + 1 + 1 + 4
)

var (
	ErrInvalidPuzzleBytes    = errors.New("invalid puzzle bytes")
	errEmptyEncodedSolutions = errors.New("encoded solutions buffer is empty")
	errEmptyDecodedSolutions = errors.New("decoded solutions buffer is empty")
	errInvalidSolutionLength = errors.New("solutions are not SolutionLength multiple")
	errInvalidVersion        = errors.New("invalid serialization version")
	errDuplicateSolution     = errors.New("duplicate solution found")
)

type Metadata struct {
	errorCode     uint8
	wasmFlag      bool
	elapsedMillis uint32
}

func (m *Metadata) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	if err := binary.Write(&buf, binary.LittleEndian, byte(metadataVersion)); err != nil {
		return buf.Bytes(), err
	}
	if err := binary.Write(&buf, binary.LittleEndian, m.errorCode); err != nil {
		return buf.Bytes(), err
	}

	var wasmFlag byte = 0
	if m.wasmFlag {
		wasmFlag = 1
	}
	if err := binary.Write(&buf, binary.LittleEndian, wasmFlag); err != nil {
		return buf.Bytes(), err
	}

	if err := binary.Write(&buf, binary.LittleEndian, m.elapsedMillis); err != nil {
		return buf.Bytes(), err
	}

	return buf.Bytes(), nil
}

func (m *Metadata) UnmarshalBinary(data []byte) error {
	if len(data) < metadataLength {
		return io.ErrShortBuffer
	}

	var offset = 0

	version := data[offset]
	if version != 1 {
		return errInvalidVersion
	}
	offset += 1

	m.errorCode = data[offset]
	offset += 1

	m.wasmFlag = data[offset] == 1
	offset += 1

	m.elapsedMillis = binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4 // nolint:ineffassign,staticcheck

	return nil
}

func (m *Metadata) ErrorCode() uint8 {
	if m == nil {
		return 0
	}

	return m.errorCode
}

func (m *Metadata) WasmFlag() bool {
	if m == nil {
		return false
	}

	return m.wasmFlag
}

func (m *Metadata) ElapsedMillis() uint32 {
	if m == nil {
		return 0
	}

	return m.elapsedMillis
}

type Solutions struct {
	Buffer   []byte
	Metadata *Metadata
}

func emptySolutions(count int) *Solutions {
	return &Solutions{
		Buffer: make([]byte, count*SolutionLength),
		Metadata: &Metadata{
			errorCode:     0,
			wasmFlag:      false,
			elapsedMillis: 0,
		},
	}
}

func (s *Solutions) UnmarshalBinary(data []byte) error {
	if len(data) < metadataLength {
		return errEmptyDecodedSolutions
	}

	s.Metadata = &Metadata{}
	if err := s.Metadata.UnmarshalBinary(data[:metadataLength]); err != nil {
		return err
	}

	s.Buffer = data[metadataLength:]

	if len(s.Buffer)%SolutionLength != 0 {
		return errInvalidSolutionLength
	}

	return nil
}

func (s *Solutions) MarshalBinary() ([]byte, error) {
	metadataBytes, err := s.Metadata.MarshalBinary()
	if err != nil {
		return nil, err
	}

	// Pre-allocate the full slice for better performance
	raw := make([]byte, len(metadataBytes)+len(s.Buffer))
	copy(raw, metadataBytes)
	copy(raw[len(metadataBytes):], s.Buffer)

	return raw, nil
}

func NewSolutions(data []byte) (*Solutions, error) {
	if len(data) == 0 {
		return nil, errEmptyEncodedSolutions
	}

	decodedBytes := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	n, err := base64.StdEncoding.Decode(decodedBytes, data)
	if err != nil {
		return nil, err
	}

	s := &Solutions{}
	if err := s.UnmarshalBinary(decodedBytes[:n]); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Solutions) String() string {
	raw, err := s.MarshalBinary()
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// map difficulty [0, 256) -> threshold [0, 2^32)
// with the reverse meaning (max difficulty -> min threshold)
// f(x) = 2^((256 - x)/8)
func thresholdFromDifficulty(difficulty uint8) uint32 {
	return uint32(math.Pow(2, (255.999999999-float64(difficulty))/8.0))
}

func (s *Solutions) CheckUnique() error {
	count := len(s.Buffer) / SolutionLength
	if count < 64 {
		return s.checkUniqueArray()
	}

	return s.checkUniqueMap()
}

func (s *Solutions) checkUniqueMap() error {
	count := len(s.Buffer) / SolutionLength
	uniqueSolutions := make(map[uint64]struct{}, count)

	for start := 0; start < len(s.Buffer); start += SolutionLength {
		solution := s.Buffer[start:(start + SolutionLength)]
		uint64Value := binary.LittleEndian.Uint64(solution)

		if _, exists := uniqueSolutions[uint64Value]; exists {
			sIndex := solution[0]
			return fmt.Errorf("duplicated solution found at index %v", sIndex)
		}

		uniqueSolutions[uint64Value] = struct{}{}
	}

	return nil
}

func (s *Solutions) checkUniqueArray() error {
	// NOTE: this is given that solutions count is kind of small
	count := len(s.Buffer) / SolutionLength
	seen := make([]uint64, count)
	foundSoFar := 0

	for start := 0; start < len(s.Buffer); start += SolutionLength {
		val := binary.LittleEndian.Uint64(s.Buffer[start : start+8])

		for i := 0; i < foundSoFar; i++ {
			if seen[i] == val {
				return errDuplicateSolution
			}
		}

		if foundSoFar < count {
			seen[foundSoFar] = val
			foundSoFar++
		}
	}
	return nil
}

func (s *Solutions) Verify(ctx context.Context, puzzleBytes []byte, difficulty uint8) (int, error) {
	if len(puzzleBytes) != PuzzleBytesLength {
		slog.WarnContext(ctx, "Puzzle bytes buffer invalid", "size", len(puzzleBytes))
		return 0, ErrInvalidPuzzleBytes
	}

	if difficulty == 0 {
		slog.Log(ctx, common.LevelTrace, "Checking solutions with zero difficulty")
		return len(s.Buffer) / SolutionLength, nil
	}

	validSolutions := 0
	threshold := thresholdFromDifficulty(difficulty)

	h, err := blake2b.New256(nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create hasher", common.ErrAttr(err))
		return 0, err
	}

	var hash [blake2b.Size256]byte

	// TODO: Shuffle solutions before checking
	// (to decrease resource exhaustion attack surface)
	for start := 0; start < len(s.Buffer); start += SolutionLength {
		solution := s.Buffer[start:(start + SolutionLength)]
		sIndex := solution[0]
		copy(puzzleBytes[PuzzleBytesLength-SolutionLength:], solution)

		h.Reset()
		if _, err := h.Write(puzzleBytes); err != nil {
			slog.WarnContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(err))
			continue
		}
		_ = h.Sum(hash[:0])

		resultInt := binary.LittleEndian.Uint32(hash[:4])

		if resultInt > threshold {
			slog.WarnContext(ctx, "Solution prefix is larger than threshold", "solution", sIndex, "prefix", resultInt,
				"threshold", threshold)
			continue
		}

		validSolutions++
	}

	slog.Log(ctx, common.LevelTrace, "Verified solutions", "count", validSolutions)

	return validSolutions, nil
}
