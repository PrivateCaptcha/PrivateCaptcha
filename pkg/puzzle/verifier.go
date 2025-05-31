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
	errEmptyPayloadPart  = errors.New("payload part is empty")
	errEmptySignature    = errors.New("empty signature")
	errEmptyPuzzle       = errors.New("empty puzzle")
	ErrSignKeyMismatch   = errors.New("signature fingerprint mismatch")
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
	puzzle     *Puzzle
	signature  *signature
	solutions  string
	puzzleData []byte
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
	solutionsStr, puzzleStr, signatureStr := parts[0], parts[1], parts[2]
	if len(solutionsStr) == 0 || len(puzzleStr) == 0 || len(signatureStr) == 0 {
		slog.WarnContext(ctx, "Invalid length of payload parts", "solutions", len(solutionsStr), "puzzle", len(puzzleStr),
			"signature", len(signatureStr))
		return nil, errEmptyPayloadPart
	}

	puzzleBytes, err := base64.StdEncoding.DecodeString(puzzleStr)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64 decode puzzle bytes", common.ErrAttr(err))
		return nil, err
	}

	if len(puzzleBytes) == 0 {
		return nil, errEmptyPuzzle
	}

	signatureBytes, err := base64.StdEncoding.DecodeString(signatureStr)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64 decode signature bytes", common.ErrAttr(err))
		return nil, err
	}

	signatureLen := len(signatureBytes)
	if signatureLen == 0 {
		return nil, errEmptySignature
	}

	p := new(Puzzle)
	if uerr := p.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", common.ErrAttr(uerr))
		return nil, uerr
	}

	s := new(signature)
	if uerr := s.UnmarshalBinary(signatureBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmashal binary signature", common.ErrAttr(uerr))
		return nil, uerr
	}

	return &VerifyPayload{
		solutions:  solutionsStr,
		puzzleData: puzzleBytes,
		puzzle:     p,
		signature:  s,
	}, nil
}

func (vp *VerifyPayload) NeedsExtraSalt() bool {
	return vp.signature.HasExtra()
}

func (vp *VerifyPayload) VerifySignature(ctx context.Context, salt *Salt, extraSalt []byte) error {
	if vp.signature.Fingerprint != salt.Fingerprint() {
		return ErrSignKeyMismatch
	}

	hasher := hmac.New(sha1.New, salt.Data())

	if _, werr := hasher.Write(vp.puzzleData); werr != nil {
		slog.WarnContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(werr))
		return werr
	}

	if vp.signature.HasExtra() && (len(extraSalt) > 0) {
		if _, werr := hasher.Write(extraSalt); werr != nil {
			slog.ErrorContext(ctx, "Failed to hash puzzle salt", "size", len(extraSalt), common.ErrAttr(werr))
			return werr
		}
	}

	actualSignature := hasher.Sum(nil)

	if !bytes.Equal(actualSignature, vp.signature.Hash) {
		slog.WarnContext(ctx, "Puzzle hash is not equal")
		return errSignatureMismatch
	}

	return nil
}

func (vp *VerifyPayload) Puzzle() *Puzzle {
	return vp.puzzle
}

func (vp *VerifyPayload) VerifySolutions(ctx context.Context) (*Metadata, VerifyError) {
	solutions, err := NewSolutions(vp.solutions)
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
