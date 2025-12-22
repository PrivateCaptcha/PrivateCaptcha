package puzzle

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	errPayloadEmpty      = errors.New("payload is empty")
	errWrongPartsNumber  = errors.New("wrong number of parts")
	errSignatureMismatch = errors.New("puzzle signature mismatch")
	errEmptyPayloadPart  = errors.New("payload part is empty")
	errEmptySignature    = errors.New("empty signature")
	errEmptyPuzzle       = errors.New("empty puzzle")
	errStubPayload       = errors.New("stub payload")
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
	OrgScopeError           VerifyError = 12
	// Add new fields _above_
	VERIFY_ERRORS_COUNT
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
	case OrgScopeError:
		return "property-org-scope"
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

type OwnerIDSource interface {
	OwnerID(ctx context.Context, tnow time.Time) (int32, *int32, error)
}

type VerifyPayload struct {
	puzzle     Puzzle
	signature  *signature
	solutions  []byte
	puzzleData []byte
}

var _ SolutionPayload = (*VerifyPayload)(nil)

type PuzzleConstraint[T any] interface {
	Puzzle
	*T
}

func ParseVerifyPayload[T any, TPuzzle PuzzleConstraint[T]](ctx context.Context, payload []byte) (*VerifyPayload, error) {
	if len(payload) == 0 {
		return nil, errPayloadEmpty
	}

	if dotsCount := bytes.Count(payload, dotBytes); dotsCount != 2 {
		slog.WarnContext(ctx, "Unexpected number of dots in payload", "dots", dotsCount)
		return nil, errWrongPartsNumber
	}

	parts := bytes.SplitN(payload, []byte{'.'}, 3)
	solutionsBytes, puzzleBytesB64, signatureBytesB64 := parts[0], parts[1], parts[2]
	if len(solutionsBytes) == 0 || len(puzzleBytesB64) == 0 || len(signatureBytesB64) == 0 {
		slog.WarnContext(ctx, "Parts of the payload are missing", "solutions", len(solutionsBytes), "puzzle", len(puzzleBytesB64), "signature", len(signatureBytesB64))
		return nil, errEmptyPayloadPart
	}

	puzzleBytesLength := base64.StdEncoding.DecodedLen(len(puzzleBytesB64))
	puzzleBytes := make([]byte, puzzleBytesLength)
	n, err := base64.StdEncoding.Decode(puzzleBytes, puzzleBytesB64)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64 decode puzzle bytes", "length", puzzleBytesLength, common.ErrAttr(err))
		return nil, err
	}

	puzzleBytes = puzzleBytes[:n]
	if len(puzzleBytes) == 0 {
		return nil, errEmptyPuzzle
	}

	signatureBytesLength := base64.StdEncoding.DecodedLen(len(signatureBytesB64))
	signatureBytes := make([]byte, signatureBytesLength)
	n, err = base64.StdEncoding.Decode(signatureBytes, signatureBytesB64)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64 decode signature bytes", "length", signatureBytesLength, common.ErrAttr(err))
		return nil, err
	}
	signatureBytes = signatureBytes[:n]
	if len(signatureBytes) == 0 {
		return nil, errEmptySignature
	}

	t := new(T)
	p := TPuzzle(t)

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
		solutions:  solutionsBytes,
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
		slog.WarnContext(ctx, "Signature fingerprint does not match salt fingerprint")
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

func (vp *VerifyPayload) Puzzle() Puzzle {
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

	solutionsActual, err := solutions.Verify(ctx, puzzleBytes, vp.puzzle.Difficulty())
	if err != nil {
		slog.WarnContext(ctx, "Failed to verify solutions", common.ErrAttr(err))
		return solutions.Metadata, InvalidSolutionError
	}

	if solutionsExpected := vp.puzzle.SolutionsCount(); solutionsActual != solutionsExpected {
		slog.WarnContext(ctx, "Invalid solutions count", "expected", solutionsExpected, "actual", solutionsActual)
		return solutions.Metadata, InvalidSolutionError
	}

	return solutions.Metadata, VerifyNoError
}

func NewStubPayload(p Puzzle) *stubVerifyPayload {
	return &stubVerifyPayload{p: p}
}

type stubVerifyPayload struct {
	p Puzzle
}

func (p *stubVerifyPayload) VerifySolutions(ctx context.Context) (*Metadata, VerifyError) {
	return &Metadata{}, TestPropertyError
}
func (p *stubVerifyPayload) Puzzle() Puzzle {
	return p.p
}
func (p *stubVerifyPayload) NeedsExtraSalt() bool {
	return false
}
func (p *stubVerifyPayload) VerifySignature(ctx context.Context, salt *Salt, extraSalt []byte) error {
	return errStubPayload
}
