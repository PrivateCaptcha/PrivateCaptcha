package puzzle

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"log/slog"

	"golang.org/x/crypto/blake2b"
)

const (
	PuzzleBytesLength = 128
	SolutionLength    = 8
)

var (
	ErrInvalidPuzzleBytes = errors.New("invalid puzzle bytes")
)

type Solutions struct {
	Buffer []byte
}

func NewSolutions(data string) (*Solutions, error) {
	if len(data) == 0 {
		return nil, errors.New("encoded solutions buffer is empty")
	}

	solutionsBytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}

	if len(solutionsBytes) == 0 {
		return nil, errors.New("decoded solutions buffer is empty")
	}

	if len(solutionsBytes)%SolutionLength != 0 {
		return nil, errors.New("solutions are not SolutionLength multiple")
	}

	return &Solutions{
		Buffer: solutionsBytes,
	}, nil
}

func thresholdFromDifficulty(difficulty uint8) uint32 {
	return 1 << ((256 - uint32(difficulty)) / 8)
}

func (s *Solutions) Verify(ctx context.Context, puzzleBytes []byte, difficulty uint8) (int, error) {
	if len(puzzleBytes) != PuzzleBytesLength {
		slog.ErrorContext(ctx, "Puzzle bytes buffer invalid", "size", len(puzzleBytes))
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
			slog.ErrorContext(ctx, "Failed to read hash prefix", "error", err, "solution", sIndex)
			continue
		}

		if resultInt > threshold {
			slog.ErrorContext(ctx, "Solution prefix is larger than threshold", "solution", sIndex, "prefix", resultInt,
				"threshold", threshold)
			continue
		}

		validSolutions++
	}

	return validSolutions, nil
}
