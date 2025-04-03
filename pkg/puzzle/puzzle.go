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
		Expiration:     time.Time{},
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

func (p *Puzzle) WriteTo(w io.Writer) (int64, error) {
	var n int64
	if err := binary.Write(w, binary.LittleEndian, p.Version); err != nil {
		return n, err
	}
	n++

	if nn, err := w.Write(p.PropertyID[:]); err != nil {
		return n + int64(nn), err
	}
	n += int64(len(p.PropertyID))

	if err := binary.Write(w, binary.LittleEndian, p.PuzzleID); err != nil {
		return n, err
	}
	n += 8

	if err := binary.Write(w, binary.LittleEndian, p.Difficulty); err != nil {
		return n, err
	}
	n++

	if err := binary.Write(w, binary.LittleEndian, p.SolutionsCount); err != nil {
		return n, err
	}
	n++

	var expiration uint32
	if !p.Expiration.IsZero() {
		expiration = uint32(p.Expiration.Unix())
	}
	if err := binary.Write(w, binary.LittleEndian, expiration); err != nil {
		return n, err
	}
	n += 4

	if nn, err := w.Write(p.UserData); err != nil {
		return n + int64(nn), err
	}
	n += int64(len(p.UserData))

	return n, nil
}

func (p *Puzzle) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	if _, err := p.WriteTo(&buf); err != nil {
		return nil, err
	}
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
