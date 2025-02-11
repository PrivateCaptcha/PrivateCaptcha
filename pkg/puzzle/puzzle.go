package puzzle

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"log/slog"
	randv2 "math/rand/v2"
	"strconv"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	PropertyIDSize = 16
	UserDataSize   = 16
	ValidityPeriod = 6 * time.Hour
	puzzleVersion  = 1
	solutionsCount = 16
)

var (
	dotBytes = []byte(".")
)

type Puzzle struct {
	Version        uint8
	Difficulty     uint8
	SolutionsCount uint8
	PropertyID     [PropertyIDSize]byte
	PuzzleID       uint64
	Expiration     time.Time
	UserData       []byte
}

func NewPuzzle(puzzleID uint64, propertyID [16]byte, difficulty uint8) *Puzzle {
	return &Puzzle{
		Version:        puzzleVersion,
		Difficulty:     difficulty,
		SolutionsCount: solutionsCount,
		PropertyID:     propertyID,
		PuzzleID:       puzzleID,
		UserData:       make([]byte, UserDataSize),
	}
}

func (p *Puzzle) Init() error {
	if _, err := io.ReadFull(rand.Reader, p.UserData); err != nil {
		return err
	}

	p.Expiration = time.Now().UTC().Add(ValidityPeriod)

	return nil
}

func RandomPuzzleID() uint64 {
	const maxTries = 10
	var puzzleID uint64

	for i := 0; (i < maxTries) && (puzzleID == 0); i++ {
		puzzleID = randv2.Uint64()
	}

	return puzzleID
}

func (p *Puzzle) IsStub() bool {
	return p.PuzzleID == 0
}

func (p *Puzzle) IsZero() bool {
	return (p.Difficulty == 0) && (p.PuzzleID == 0) && p.Expiration.IsZero()
}

func (p *Puzzle) PuzzleIDString() string {
	return strconv.FormatUint(p.PuzzleID, 16)
}

func (p *Puzzle) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	binary.Write(&buf, binary.LittleEndian, p.Version)
	binary.Write(&buf, binary.LittleEndian, p.PropertyID)
	binary.Write(&buf, binary.LittleEndian, p.PuzzleID)
	binary.Write(&buf, binary.LittleEndian, p.Difficulty)
	binary.Write(&buf, binary.LittleEndian, p.SolutionsCount)

	var expiration uint32
	if !p.Expiration.IsZero() {
		expiration = uint32(p.Expiration.Unix())
	}
	binary.Write(&buf, binary.LittleEndian, expiration)

	binary.Write(&buf, binary.LittleEndian, p.UserData)

	// NOTE: we do NOT serialize salt

	return buf.Bytes(), nil
}

func (p *Puzzle) UnmarshalBinary(data []byte) error {
	if len(data) < (PropertyIDSize + 8 + UserDataSize + 7) {
		return io.ErrShortBuffer
	}

	var offset int

	p.Version = data[0]
	offset += 1

	copy(p.PropertyID[:], data[offset:offset+PropertyIDSize])
	offset += PropertyIDSize

	p.PuzzleID = binary.LittleEndian.Uint64(data[offset : offset+8])
	offset += 8

	p.Difficulty = data[offset]
	offset += 1

	p.SolutionsCount = data[offset]
	offset += 1

	unixExpiration := int64(binary.LittleEndian.Uint32(data[offset : offset+4]))
	if unixExpiration != 0 {
		p.Expiration = time.Unix(unixExpiration, 0)
	}
	offset += 4

	p.UserData = make([]byte, UserDataSize)
	copy(p.UserData, data[offset:offset+UserDataSize])
	//offset += UserDataSize

	return nil
}

type PuzzlePayload struct {
	puzzleBase64    []byte
	signatureBase64 []byte
}

func (p *Puzzle) Serialize(ctx context.Context, salt *Salt, extraSalt []byte) (*PuzzlePayload, error) {
	puzzleBytes, err := p.MarshalBinary()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize puzzle", common.ErrAttr(err))
		return nil, err
	}

	hasher := hmac.New(sha1.New, salt.Data())
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.ErrorContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(werr))
		return nil, werr
	}

	if len(extraSalt) > 0 {
		if _, werr := hasher.Write(extraSalt); werr != nil {
			slog.ErrorContext(ctx, "Failed to hash puzzle salt", "size", len(extraSalt), common.ErrAttr(werr))
		}
	}

	hash := hasher.Sum(nil)

	sign := newSignature(hash, salt, extraSalt)
	signatureBytes, err := sign.MarshalBinary()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize signature", common.ErrAttr(err))
		return nil, err
	}

	puzzleBase64Len := base64.StdEncoding.EncodedLen(len(puzzleBytes))
	signatureBase64Len := base64.StdEncoding.EncodedLen(len(signatureBytes))

	pp := &PuzzlePayload{
		puzzleBase64:    make([]byte, puzzleBase64Len),
		signatureBase64: make([]byte, signatureBase64Len),
	}

	base64.StdEncoding.Encode(pp.puzzleBase64, puzzleBytes)
	base64.StdEncoding.Encode(pp.signatureBase64, signatureBytes)

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
