package puzzle

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errPayloadEmpty     = errors.New("payload is empty")
	errWrongPartsNumber = errors.New("wrong number of parts")
)

type VerifyError int

const (
	VerifyNoError           VerifyError = 0
	VerifyErrorOther        VerifyError = 1
	DuplicateSolutionsError VerifyError = 2
	InvalidSolutionError    VerifyError = 3
	ParseResponseError      VerifyError = 4
	PuzzleExpiredError      VerifyError = 5
	InvalidPropertyError    VerifyError = 6
	WrongOwnerError         VerifyError = 7
	VerifiedBeforeError     VerifyError = 8
)

type OwnerIDSource interface {
	OwnerID(ctx context.Context) (int32, error)
}

type Verifier interface {
	Verify(ctx context.Context, payload string, expectedOwner OwnerIDSource, tnow time.Time) (VerifyError, error)
}

func ParseSolutions(ctx context.Context, payload string, salt []byte) (string, []byte, error) {
	if len(payload) == 0 {
		return "", nil, errPayloadEmpty
	}

	parts := strings.Split(payload, ".")
	if len(parts) != 3 {
		return "", nil, errWrongPartsNumber
	}

	solutionsData, puzzleData, signature := parts[0], parts[1], parts[2]

	puzzleBytes, err := base64.StdEncoding.DecodeString(puzzleData)
	if err != nil {
		return "", nil, err
	}

	hasher := hmac.New(sha1.New, salt)
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.WarnContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(err))
		return "", nil, werr
	}

	hash := hasher.Sum(nil)

	requestHash, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		slog.WarnContext(ctx, "Failed to decode signature bytes", common.ErrAttr(err))
		return "", nil, err
	}

	if !bytes.Equal(hash, requestHash) {
		slog.WarnContext(ctx, "Puzzle hash is not equal")
		return "", nil, err
	}

	return solutionsData, puzzleBytes, nil
}

func VerifySolutions(ctx context.Context, p *Puzzle, puzzleBytes []byte, solutionsData string) VerifyError {
	solutions, err := NewSolutions(solutionsData)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode solutions bytes", common.ErrAttr(err))
		return ParseResponseError
	}

	if uerr := solutions.CheckUnique(ctx); uerr != nil {
		slog.ErrorContext(ctx, "Solutions are not unique", common.ErrAttr(uerr))
		return DuplicateSolutionsError
	}

	if len(puzzleBytes) < PuzzleBytesLength {
		extendedPuzzleBytes := make([]byte, PuzzleBytesLength)
		copy(extendedPuzzleBytes, puzzleBytes)
		puzzleBytes = extendedPuzzleBytes
	}

	solutionsCount, err := solutions.Verify(ctx, puzzleBytes, p.Difficulty)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to verify solutions", common.ErrAttr(err))
		return InvalidSolutionError
	}

	if solutionsCount != int(p.SolutionsCount) {
		slog.WarnContext(ctx, "Invalid solutions count", "expected", p.SolutionsCount, "actual", solutionsCount)
		return InvalidSolutionError
	}

	return VerifyNoError
}
