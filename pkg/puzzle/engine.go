package puzzle

import (
	"context"
	"encoding"
	"time"
)

type VerifyResult struct {
	UserID     int32
	OrgID      int32
	PropertyID int32
	PuzzleID   uint64
	Error      VerifyError
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Domain     string
}

func NewVerifyResult(verr VerifyError) *VerifyResult {
	return &VerifyResult{Error: verr}
}

func (vr *VerifyResult) Valid() bool {
	// NOTE: do NOT check puzzleID
	return (vr != nil) &&
		(vr.UserID > 0) &&
		(vr.OrgID > 0) &&
		(vr.PropertyID > 0) &&
		!vr.CreatedAt.IsZero()
}

func (vr *VerifyResult) Success() bool {
	return (vr.Error == VerifyNoError) ||
		(vr.Error == MaintenanceModeError)
}

func (vr *VerifyResult) SetError(verr VerifyError) {
	vr.Error = verr
}

func (vr *VerifyResult) ErrorsToStrings() []string {
	if vr.Error == VerifyNoError {
		return []string{}
	}

	result := make([]string, 0, 1)

	if vr.Error != VerifyNoError {
		result = append(result, vr.Error.String())
	}

	return result
}

type Puzzle interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler

	Init(validityPeriod time.Duration) error
	HashKey() uint64
	IsStub() bool
	IsZero() bool
	Challenge() Challenge
	Difficulty() uint8
	SolutionsCount() int
	PuzzleID() uint64
	PropertyID() [PropertyIDSize]byte
	Expiration() time.Time
	Serialize(ctx context.Context, salt *Salt, extraSalt []byte) (*PuzzlePayload, error)
}

type SolutionPayload interface {
	VerifySolutions(ctx context.Context) (*Metadata, VerifyError)
	Puzzle() Puzzle
	NeedsExtraSalt() bool
	VerifySignature(ctx context.Context, salt *Salt, extraSalt []byte) error
}
