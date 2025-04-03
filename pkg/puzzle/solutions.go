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

	var offset int = 0

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
	offset += 4 // nolint:ineffassign

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

func NewSolutions(data string) (*Solutions, error) {
	if len(data) == 0 {
		return nil, errEmptyEncodedSolutions
	}

	decodedBytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}

	if len(decodedBytes) == 0 {
		return nil, errEmptyDecodedSolutions
	}

	metadata := &Metadata{}
	if err := metadata.UnmarshalBinary(decodedBytes[:metadataLength]); err != nil {
		return nil, err
	}

	solutionsBytes := decodedBytes[metadataLength:]

	if len(solutionsBytes)%SolutionLength != 0 {
		return nil, errInvalidSolutionLength
	}

	return &Solutions{
		Buffer:   solutionsBytes,
		Metadata: metadata,
	}, nil
}

func (s *Solutions) String() string {
	var buf bytes.Buffer
	if metadataBytes, err := s.Metadata.MarshalBinary(); err == nil {
		_, _ = buf.Write(metadataBytes)
	}
	_, _ = buf.Write(s.Buffer)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// map difficulty [0, 256) -> threshold [0, 2^32)
// with the reverse meaning (max difficulty -> min threshold)
// f(x) = 2^((256 - x)/8)
func thresholdFromDifficulty(difficulty uint8) uint32 {
	return uint32(math.Pow(2, (255.999999999-float64(difficulty))/8.0))
}

func (s *Solutions) CheckUnique() error {
	uniqueSolutions := make(map[uint64]bool)

	for start := 0; start < len(s.Buffer); start += SolutionLength {
		solution := s.Buffer[start:(start + SolutionLength)]
		uint64Value := binary.LittleEndian.Uint64(solution)

		if _, ok := uniqueSolutions[uint64Value]; ok {
			sIndex := solution[0]
			return fmt.Errorf("duplicated solution found at index %v", sIndex)
		}

		uniqueSolutions[uint64Value] = true
	}

	return nil
}

func (s *Solutions) Verify(ctx context.Context, puzzleBytes []byte, difficulty uint8) (int, error) {
	if len(puzzleBytes) != PuzzleBytesLength {
		slog.WarnContext(ctx, "Puzzle bytes buffer invalid", "size", len(puzzleBytes))
		return 0, ErrInvalidPuzzleBytes
	}

	validSolutions := 0
	threshold := thresholdFromDifficulty((difficulty))

	for start := 0; start < len(s.Buffer); start += SolutionLength {
		solution := s.Buffer[start:(start + SolutionLength)]
		sIndex := solution[0]
		copy(puzzleBytes[PuzzleBytesLength-SolutionLength:], solution)

		hash := blake2b.Sum256(puzzleBytes)
		var resultInt uint32
		err := binary.Read(bytes.NewReader(hash[:4]), binary.LittleEndian, &resultInt)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read hash prefix", "solution", sIndex, "size", 4, common.ErrAttr(err))
			continue
		}

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
