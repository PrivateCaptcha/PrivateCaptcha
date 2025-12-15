package puzzle

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"hash/fnv"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/rs/xid"
)

const (
	PropertyIDSize        = 16
	UserDataSize          = 16
	DefaultValidityPeriod = 30 * time.Minute
	puzzleVersion         = 1
	solutionsCount        = 16
)

var (
	dotBytes = []byte(".")
)

type ComputePuzzle struct {
	version        uint8
	difficulty     uint8
	solutionsCount uint8
	propertyID     [PropertyIDSize]byte
	puzzleID       uint64
	expiration     time.Time
	userData       []byte
}

var _ Puzzle = (*ComputePuzzle)(nil)

func NewComputePuzzle(puzzleID uint64, propertyID [PropertyIDSize]byte, difficulty uint8) *ComputePuzzle {
	return &ComputePuzzle{
		version:        puzzleVersion,
		difficulty:     difficulty,
		solutionsCount: solutionsCount,
		propertyID:     propertyID,
		puzzleID:       puzzleID,
		userData:       make([]byte, UserDataSize),
		expiration:     time.Time{},
	}
}

func (p *ComputePuzzle) Init(validityPeriod time.Duration) error {
	if _, err := io.ReadFull(rand.Reader, p.userData); err != nil {
		return err
	}

	p.expiration = time.Now().UTC().Add(validityPeriod)

	return nil
}

func (p *ComputePuzzle) PuzzleID() uint64                 { return p.puzzleID }
func (p *ComputePuzzle) Difficulty() uint8                { return p.difficulty }
func (p *ComputePuzzle) SolutionsCount() int              { return int(p.solutionsCount) }
func (p *ComputePuzzle) Expiration() time.Time            { return p.expiration }
func (p *ComputePuzzle) PropertyID() [PropertyIDSize]byte { return p.propertyID }

func (p *ComputePuzzle) HashKey() uint64 {
	hasher := fnv.New64a()

	_, _ = hasher.Write(p.propertyID[:])

	var pidBytes [8]byte
	binary.LittleEndian.PutUint64(pidBytes[:], p.puzzleID)
	_, _ = hasher.Write(pidBytes[:])
	_, _ = hasher.Write(p.userData[:])

	return hasher.Sum64()
}

func NextPuzzleID() uint64 {
	hasher := fnv.New64a()

	// we need to compress xid as it's 12 bytes
	_, _ = hasher.Write(xid.New().Bytes())

	return hasher.Sum64()
}

func (p *ComputePuzzle) IsStub() bool {
	return p.puzzleID == 0
}

func (p *ComputePuzzle) IsZero() bool {
	return (p.difficulty == 0) && (p.puzzleID == 0) && p.expiration.IsZero()
}

func (p *ComputePuzzle) WriteTo(w io.Writer) (int64, error) {
	var n int64
	if err := binary.Write(w, binary.LittleEndian, p.version); err != nil {
		return n, err
	}
	n++

	if nn, err := w.Write(p.propertyID[:]); err != nil {
		return n + int64(nn), err
	}
	n += int64(len(p.propertyID))

	if err := binary.Write(w, binary.LittleEndian, p.puzzleID); err != nil {
		return n, err
	}
	n += 8

	if err := binary.Write(w, binary.LittleEndian, p.difficulty); err != nil {
		return n, err
	}
	n++

	if err := binary.Write(w, binary.LittleEndian, p.solutionsCount); err != nil {
		return n, err
	}
	n++

	var expiration uint32
	if !p.expiration.IsZero() {
		expiration = uint32(p.expiration.Unix())
	}
	if err := binary.Write(w, binary.LittleEndian, expiration); err != nil {
		return n, err
	}
	n += 4

	if nn, err := w.Write(p.userData); err != nil {
		return n + int64(nn), err
	}
	n += int64(len(p.userData))

	return n, nil
}

func (p *ComputePuzzle) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	if _, err := p.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (p *ComputePuzzle) UnmarshalBinary(data []byte) error {
	if len(data) < (PropertyIDSize + 8 + UserDataSize + 7) {
		return io.ErrShortBuffer
	}

	var offset int

	p.version = data[0]
	offset += 1

	copy(p.propertyID[:], data[offset:offset+PropertyIDSize])
	offset += PropertyIDSize

	p.puzzleID = binary.LittleEndian.Uint64(data[offset : offset+8])
	offset += 8

	p.difficulty = data[offset]
	offset += 1

	p.solutionsCount = data[offset]
	offset += 1

	unixExpiration := int64(binary.LittleEndian.Uint32(data[offset : offset+4]))
	if unixExpiration != 0 {
		p.expiration = time.Unix(unixExpiration, 0)
	}
	offset += 4

	p.userData = make([]byte, UserDataSize)
	copy(p.userData, data[offset:offset+UserDataSize])
	//offset += UserDataSize

	return nil
}

type PuzzlePayload struct {
	puzzleBase64    []byte
	signatureBase64 []byte
}

func (p *ComputePuzzle) Serialize(ctx context.Context, salt *Salt, extraSalt []byte) (*PuzzlePayload, error) {
	// First write to hasher
	hasher := hmac.New(sha1.New, salt.Data())
	puzzleSize, err := p.WriteTo(hasher)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle to hasher", common.ErrAttr(err))
		return nil, err
	}

	if len(extraSalt) > 0 {
		if _, werr := hasher.Write(extraSalt); werr != nil {
			slog.ErrorContext(ctx, "Failed to hash puzzle salt", "size", len(extraSalt), common.ErrAttr(werr))
			return nil, werr
		}
	}

	hash := hasher.Sum(nil)
	sign := newSignature(hash, salt, extraSalt)

	puzzleBase64Len := base64.StdEncoding.EncodedLen(int(puzzleSize))
	signatureBase64Len := base64.StdEncoding.EncodedLen(sign.BinarySize())

	pp := &PuzzlePayload{}

	pp.puzzleBase64 = make([]byte, puzzleBase64Len)
	// Write puzzle to base64
	puzzleWriter := base64.NewEncoder(base64.StdEncoding, bytes.NewBuffer(pp.puzzleBase64[:0]))
	if _, err := p.WriteTo(puzzleWriter); err != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle", common.ErrAttr(err))
		return nil, err
	}
	if err := puzzleWriter.Close(); err != nil {
		slog.ErrorContext(ctx, "Failed to flush puzzle encoder", common.ErrAttr(err))
		return nil, err
	}

	pp.signatureBase64 = make([]byte, signatureBase64Len)
	// Write signature to base64
	signatureWriter := base64.NewEncoder(base64.StdEncoding, bytes.NewBuffer(pp.signatureBase64[:0]))
	if _, err := sign.WriteTo(signatureWriter); err != nil {
		slog.ErrorContext(ctx, "Failed to write signature", common.ErrAttr(err))
		return nil, err
	}
	if err := signatureWriter.Close(); err != nil {
		slog.ErrorContext(ctx, "Failed to flush signature encoder", common.ErrAttr(err))
		return nil, err
	}

	return pp, nil
}

func (pp *PuzzlePayload) Write(w io.Writer) error {
	if _, werr := w.Write(pp.puzzleBase64); werr != nil {
		return werr
	}

	_, _ = w.Write(dotBytes)

	if _, werr := w.Write(pp.signatureBase64); werr != nil {
		return werr
	}

	return nil
}

func (pp *PuzzlePayload) Size() int {
	return len(pp.signatureBase64) + len(pp.puzzleBase64) + len(dotBytes)
}

func (pp *PuzzlePayload) IsSuffixFor(data []byte) bool {
	dlen := len(data)
	plen := len(pp.puzzleBase64)
	slen := len(pp.signatureBase64)

	start := dlen - (plen + 1 + slen)
	if start < 0 {
		return false
	}

	if !bytes.Equal(pp.puzzleBase64, data[start:(start+plen)]) {
		return false
	}

	if data[start+plen] != dotBytes[0] {
		return false
	}

	if !bytes.Equal(pp.signatureBase64, data[(start+plen+1):(start+plen+1+slen)]) {
		return false
	}

	return true
}

var ValidityDurations = []time.Duration{
	5 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
	2 * 24 * time.Hour,
	7 * 24 * time.Hour,
}

func ValidityIntervalToIndex(period time.Duration) int {
	for i, d := range ValidityDurations {
		if d == period {
			return i
		}
	}

	return 3
}

func ValidityIntervalFromIndex(ctx context.Context, index string) time.Duration {
	i, err := strconv.Atoi(index)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert validity period", "value", index, common.ErrAttr(err))
		return DefaultValidityPeriod
	}

	if i >= 0 && i < len(ValidityDurations) {
		return ValidityDurations[i]
	}

	slog.WarnContext(ctx, "Invalid validity period index", "index", i)
	return DefaultValidityPeriod
}
