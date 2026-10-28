package puzzle

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"log/slog"
	"math"
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
	errInvalidEncoding   = errors.New("invalid payload encoding")
	errInvalidSolutions  = errors.New("invalid solutions encoding or count")
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
	SolutionTimingError     VerifyError = 13
	// Add new fields _above_
	VERIFY_ERRORS_COUNT VerifyError = iota
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
	case SolutionTimingError:
		return "solution-timing"
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
	solutions  *Solutions
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
		slog.WarnContext(
			ctx,
			"Parts of the payload are missing",
			"solutions",
			len(solutionsBytes),
			"puzzle",
			len(puzzleBytesB64),
			"signature",
			len(signatureBytesB64),
		)
		return nil, errEmptyPayloadPart
	}
	if len(puzzleBytesB64) > base64.StdEncoding.EncodedLen(maxPuzzleBodySize) ||
		len(signatureBytesB64) != base64.StdEncoding.EncodedLen(signatureSize) {
		return nil, errInvalidEncoding
	}

	puzzleBytesLength := base64.StdEncoding.DecodedLen(len(puzzleBytesB64))
	puzzleBytes := make([]byte, puzzleBytesLength)
	n, err := base64.StdEncoding.Decode(puzzleBytes, puzzleBytesB64)
	if err != nil {
		slog.WarnContext(ctx, "Failed to base64 decode puzzle bytes", "length", puzzleBytesLength, common.ErrAttr(err))
		return nil, err
	}

	puzzleBytes = puzzleBytes[:n]
	if len(puzzleBytes) > maxPuzzleBodySize {
		return nil, errInvalidEncoding
	}
	if len(puzzleBytes) == 0 {
		return nil, errEmptyPuzzle
	}

	t := new(T)
	p := TPuzzle(t)

	if uerr := p.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", common.ErrAttr(uerr))
		return nil, uerr
	}
	if len(solutionsBytes) != base64.StdEncoding.EncodedLen(metadataLength+p.SolutionsCount()*SolutionLength) {
		return nil, errInvalidSolutions
	}
	solutions, err := NewSolutions(solutionsBytes)
	if err != nil {
		slog.WarnContext(ctx, "Failed to decode solutions bytes", common.ErrAttr(err))
		return nil, err
	}
	if len(solutions.Buffer) != p.SolutionsCount()*SolutionLength {
		slog.WarnContext(ctx, "Invalid solutions encoding or count", "expected", p.SolutionsCount(), "actual", len(solutions.Buffer)/SolutionLength)
		return nil, errInvalidSolutions
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

	s := new(signature)
	if uerr := s.UnmarshalBinary(signatureBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmashal binary signature", common.ErrAttr(uerr))
		return nil, uerr
	}

	return &VerifyPayload{
		solutions:  solutions,
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

	if !hmac.Equal(actualSignature, vp.signature.Hash) {
		slog.WarnContext(ctx, "Puzzle hash is not equal")
		return errSignatureMismatch
	}

	return nil
}

func (vp *VerifyPayload) Puzzle() Puzzle {
	return vp.puzzle
}

func (vp *VerifyPayload) VerifySolutions(ctx context.Context, minSolveTimeAtMedium, maxSolveTime *time.Duration) (*Metadata, VerifyError) {
	solutions := vp.solutions
	if solutions == nil || len(solutions.Buffer) != vp.puzzle.SolutionsCount()*SolutionLength ||
		(vp.puzzle.Challenge() == ChallengeArgon2ID && vp.puzzle.SolutionsCount() != Argon2IDSolutionsCount) {
		slog.WarnContext(ctx, "Invalid solutions count")
		return nil, ParseResponseError
	}

	if uerr := solutions.CheckUnique(); uerr != nil {
		slog.WarnContext(ctx, "Solutions are not unique", common.ErrAttr(uerr))
		return solutions.Metadata, DuplicateSolutionsError
	}

	var solutionsActual int
	var err error
	if vp.puzzle.Challenge() == ChallengeArgon2ID {
		solutionsActual, err = solutions.VerifyArgon2ID(ctx, vp.puzzleData, vp.puzzle.Difficulty(), Argon2IDMemoryKiB)
	} else {
		puzzleBytes := vp.puzzleData
		if len(puzzleBytes) < PuzzleBytesLength {
			extendedPuzzleBytes := make([]byte, PuzzleBytesLength)
			copy(extendedPuzzleBytes, puzzleBytes)
			puzzleBytes = extendedPuzzleBytes
		}
		solutionsActual, err = solutions.VerifyBlake2b(ctx, puzzleBytes, vp.puzzle.Difficulty())
	}
	if err != nil {
		slog.WarnContext(ctx, "Failed to verify solutions", common.ErrAttr(err))
		return solutions.Metadata, InvalidSolutionError
	}

	if solutionsExpected := vp.puzzle.SolutionsCount(); solutionsActual != solutionsExpected {
		slog.WarnContext(ctx, "Invalid solutions count", "expected", solutionsExpected, "actual", solutionsActual)
		return solutions.Metadata, InvalidSolutionError
	}

	verr := verifySolveTime(ctx, vp.puzzle, solutions.Metadata, minSolveTimeAtMedium, maxSolveTime)
	if verr != VerifyNoError {
		return solutions.Metadata, verr
	}

	return solutions.Metadata, VerifyNoError
}

// Full-puzzle solve times in seconds, measured at each bucket's lower difficulty.
// Source: BenchmarkSolverDifficultyBuckets in solver_test.go.
// Run: make bench-puzzle-solver EXTRA_TEST_FLAGS='-bench=BenchmarkSolverDifficultyBuckets'
var blake2bSolveTimeSeconds = [...]float64{
	0.069360138,  // Measured at 136; bucket 136-151.
	0.279922708,  // Measured at 152; bucket 152-167 (small).
	1.053332916,  // Measured at 168; bucket 168-183 (medium).
	4.002525749,  // Measured at 184; bucket 184-199 (high).
	21.181988666, // Measured at 200; bucket 200-255 (capped at 200).
}

func verifySolveTime(ctx context.Context, p Puzzle, metadata *Metadata, minSolveTimeAtMedium, maxSolveTime *time.Duration) VerifyError {
	// The widget packs signed JS elapsed milliseconds into four bytes. Negative
	// values arrive as two's-complement uint32 values.
	elapsedMillis := int32(metadata.ElapsedMillis())
	if elapsedMillis < 0 {
		slog.WarnContext(ctx, "Reported solve time is negative", "elapsedMillis", elapsedMillis, common.PuzzleIDAttr(p.PuzzleID()))
		return SolutionTimingError
	}
	if p.SolutionsCount() == 0 || p.IsZero() {
		return VerifyNoError
	}
	reportedElapsed := time.Duration(elapsedMillis) * time.Millisecond
	elapsed := reportedElapsed
	if maxSolveTime != nil {
		// Use the shorter client/server interval for the minimum-time comparison.
		elapsed = min(elapsed, *maxSolveTime)
		if reportedElapsed > *maxSolveTime {
			slog.WarnContext(ctx, "Reported solve time exceeds puzzle age", "elapsedMillis", elapsedMillis,
				"puzzleAgeMillis", maxSolveTime.Seconds()*1000, common.PuzzleIDAttr(p.PuzzleID()))
			return SolutionTimingError
		}
	}
	if minSolveTimeAtMedium == nil || p.Challenge() != ChallengeBlake2b {
		return VerifyNoError
	}
	const minDifficulty = uint8(common.DifficultyLevelSmall - common.DifficultyDelta)
	if p.Difficulty() < minDifficulty {
		return VerifyNoError
	}
	// Recorded times only gate minimum-time eligibility, using the preceding measured
	// bucket and the last bucket for harder puzzles. Subsecond benchmarks are unreliable.
	bucket := min(int(p.Difficulty()-minDifficulty)/common.DifficultyDelta, len(blake2bSolveTimeSeconds)-1)
	if blake2bSolveTimeSeconds[bucket] < 1 {
		return VerifyNoError
	}
	if elapsed == 0 {
		return SolutionTimingError
	}
	// Difficulty scales as 2^(d/8), so relative work versus medium is
	// 2^((d-medium)/8). Use the actual difficulty rather than the bucket edge;
	// cap it at the highest recorded difficulty to avoid unmeasured extrapolation.
	// Every eight steps double the minimum; medium's factor is exactly one.
	// Keep fractional seconds throughout: rounding to milliseconds would lose
	// submillisecond minima, while scaling time.Duration could overflow.
	const maxDifficulty = int(minDifficulty) + (len(blake2bSolveTimeSeconds)-1)*common.DifficultyDelta
	difficulty := min(int(p.Difficulty()), maxDifficulty)
	workFactor := math.Exp2(float64(difficulty-int(common.DifficultyLevelMedium)) / 8.0)
	minimumSeconds := minSolveTimeAtMedium.Seconds() * workFactor
	if elapsed.Seconds() < minimumSeconds {
		return SolutionTimingError
	}
	return VerifyNoError
}

func NewStubPayload(p Puzzle) *stubVerifyPayload {
	return &stubVerifyPayload{p: p}
}

type stubVerifyPayload struct {
	p Puzzle
}

func (p *stubVerifyPayload) VerifySolutions(ctx context.Context, minSolveTimeAtMedium, maxSolveTime *time.Duration) (*Metadata, VerifyError) {
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
