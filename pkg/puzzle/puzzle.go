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

const (
	flagInitialized uint8 = 1 << iota
	flagStub
)

var (
	dotBytes = []byte(".")
)

type Puzzle struct {
	Version        uint8
	Flags          uint8
	Difficulty     uint8
	SolutionsCount uint8
	PropertyID     [PropertyIDSize]byte
	PuzzleID       uint64
	Expiration     time.Time
	UserData       []byte
	Salt           []byte
}

func NewPuzzle() *Puzzle {
	return &Puzzle{
		Version:        puzzleVersion,
		Flags:          flagStub,
		Difficulty:     0,
		SolutionsCount: solutionsCount,
		PropertyID:     [16]byte{},
		PuzzleID:       0,
		UserData:       make([]byte, UserDataSize),
		Expiration:     time.Time{},
		Salt:           nil,
	}
}

func (p *Puzzle) Init(propertyID [16]byte, difficulty uint8, salt []byte) error {
	const maxTries = 10
	var puzzleID uint64

	for i := 0; (i < maxTries) && (puzzleID == 0); i++ {
		puzzleID = randv2.Uint64()
	}

	p.PropertyID = propertyID
	p.PuzzleID = puzzleID
	p.Difficulty = difficulty
	p.Expiration = time.Now().UTC().Add(ValidityPeriod)
	p.Salt = salt

	if _, err := io.ReadFull(rand.Reader, p.UserData); err != nil {
		return err
	}

	var flags uint8
	flags |= flagInitialized

	if len(salt) == 0 {
		flags |= flagStub
	}

	p.Flags = flags

	return nil
}

func (p *Puzzle) IsStub() bool {
	return (p.Flags & flagStub) != 0
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
	binary.Write(&buf, binary.LittleEndian, p.Flags)
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
	if len(data) < (PropertyIDSize + 8 + UserDataSize + 7 + 1) {
		return io.ErrShortBuffer
	}

	var offset int

	p.Version = data[0]
	offset += 1

	p.Flags = data[offset]
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
	puzzleBase64 string
	hashBase64   string
}

func (p *Puzzle) Serialize(ctx context.Context, salt []byte) (*PuzzlePayload, error) {
	puzzleBytes, err := p.MarshalBinary()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize puzzle", common.ErrAttr(err))
		return nil, err
	}

	hasher := hmac.New(sha1.New, salt)
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.ErrorContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(werr))
	}

	if !p.IsStub() && (len(p.Salt) > 0) {
		if _, werr := hasher.Write(p.Salt); werr != nil {
			slog.ErrorContext(ctx, "Failed to hash puzzle salt", "size", len(p.Salt), common.ErrAttr(werr))
		}
	}

	hash := hasher.Sum(nil)

	return &PuzzlePayload{
		puzzleBase64: base64.StdEncoding.EncodeToString(puzzleBytes),
		hashBase64:   base64.StdEncoding.EncodeToString(hash),
	}, nil
}

func (pp *PuzzlePayload) Write(w io.Writer) error {
	if _, werr := w.Write([]byte(pp.puzzleBase64)); werr != nil {
		return werr
	}

	_, _ = w.Write(dotBytes)

	if _, werr := w.Write([]byte(pp.hashBase64)); werr != nil {
		return werr
	}

	return nil
}
