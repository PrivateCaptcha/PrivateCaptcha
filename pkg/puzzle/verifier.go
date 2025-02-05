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

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errPayloadEmpty      = errors.New("payload is empty")
	errWrongPartsNumber  = errors.New("wrong number of parts")
	errSignatureMismatch = errors.New("puzzle signature mismatch")
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
	MaintenanceModeError    VerifyError = 9
	TestPropertyError       VerifyError = 10
	IntegrityError          VerifyError = 11
)

func (verr VerifyError) String() string {
	switch verr {
	case VerifyNoError:
		return "no-error"
	case VerifyErrorOther:
		return "error-other"
	case DuplicateSolutionsError:
		return "solution-duplicates"
	case InvalidSolutionError:
		return "solution-invalid"
	case ParseResponseError:
		return "solution-bad-format"
	case PuzzleExpiredError:
		return "puzzle-expired"
	case InvalidPropertyError:
		return "property-invalid"
	case WrongOwnerError:
		return "property-owner-mismatch"
	case VerifiedBeforeError:
		return "solution-verified-before"
	case MaintenanceModeError:
		return "maintenance-mode"
	case TestPropertyError:
		return "property-test"
	case IntegrityError:
		return "integrity-error"
	default:
		return "error"
	}
}

func ErrorCodesToStrings(verr []VerifyError) []string {
	if len(verr) == 0 {
		return nil
	}

	result := make([]string, 0, len(verr))

	for _, err := range verr {
		result = append(result, err.String())
	}

	return result
}

type OwnerIDSource interface {
	OwnerID(ctx context.Context) (int32, error)
}

type VerifyPayload struct {
	puzzle        *Puzzle
	solutionsData string
	puzzleData    []byte
	signature     []byte
}

func ParseVerifyPayload(ctx context.Context, payload string) (*VerifyPayload, error) {
	if len(payload) == 0 {
		return nil, errPayloadEmpty
	}

	if dotsCount := strings.Count(payload, "."); dotsCount != 2 {
		slog.WarnContext(ctx, "Unexpected number of dots in payload", "dots", dotsCount)
		return nil, errWrongPartsNumber
	}

	parts := strings.Split(payload, ".")
	solutionsData, puzzleData, signature := parts[0], parts[1], parts[2]

	puzzleBytes, err := base64.StdEncoding.DecodeString(puzzleData)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64 decode puzzle bytes", common.ErrAttr(err))
		return nil, err
	}

	requestHash, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64 decode signature bytes", common.ErrAttr(err))
		return nil, err
	}

	p := new(Puzzle)
	if uerr := p.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", common.ErrAttr(uerr))
		return nil, uerr
	}

	return &VerifyPayload{
		solutionsData: solutionsData,
		puzzleData:    puzzleBytes,
		signature:     requestHash,
		puzzle:        p,
	}, nil
}

func (vp *VerifyPayload) VerifySignature(ctx context.Context, commonSalt, puzzleSalt []byte) error {
	hasher := hmac.New(sha1.New, commonSalt)

	if _, werr := hasher.Write(vp.puzzleData); werr != nil {
		slog.WarnContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(werr))
		return werr
	}

	if len(puzzleSalt) > 0 {
		if _, werr := hasher.Write(puzzleSalt); werr != nil {
			slog.ErrorContext(ctx, "Failed to hash puzzle salt", "size", len(puzzleSalt), common.ErrAttr(werr))
			return werr
		}
	}

	actualSignature := hasher.Sum(nil)

	if !bytes.Equal(actualSignature, vp.signature) {
		slog.WarnContext(ctx, "Puzzle hash is not equal")
		return errSignatureMismatch
	}

	return nil
}

func (vp *VerifyPayload) Puzzle() *Puzzle {
	return vp.puzzle
}

func (vp *VerifyPayload) VerifySolutions(ctx context.Context) (*Metadata, VerifyError) {
	solutions, err := NewSolutions(vp.solutionsData)
	if err != nil {
		slog.WarnContext(ctx, "Failed to decode solutions bytes", common.ErrAttr(err))
		return nil, ParseResponseError
	}

	if uerr := solutions.CheckUnique(); uerr != nil {
		slog.WarnContext(ctx, "Solutions are not unique", common.ErrAttr(uerr))
		return solutions.Metadata, DuplicateSolutionsError
	}

	puzzleBytes := vp.puzzleData
	if len(puzzleBytes) < PuzzleBytesLength {
		extendedPuzzleBytes := make([]byte, PuzzleBytesLength)
		copy(extendedPuzzleBytes, puzzleBytes)
		puzzleBytes = extendedPuzzleBytes
	}

	solutionsCount, err := solutions.Verify(ctx, puzzleBytes, vp.puzzle.Difficulty)
	if err != nil {
		slog.WarnContext(ctx, "Failed to verify solutions", common.ErrAttr(err))
		return solutions.Metadata, InvalidSolutionError
	}

	if solutionsCount != int(vp.puzzle.SolutionsCount) {
		slog.WarnContext(ctx, "Invalid solutions count", "expected", vp.puzzle.SolutionsCount, "actual", solutionsCount)
		return solutions.Metadata, InvalidSolutionError
	}

	return solutions.Metadata, VerifyNoError
}
