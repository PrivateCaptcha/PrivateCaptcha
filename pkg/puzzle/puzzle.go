package puzzle

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const (
	NonceSize      = 16
	PropertyIDSize = 16
	UserDataSize   = 16
)

type Puzzle struct {
	Version        uint8
	Difficulty     uint8
	SolutionsCount uint8
	Nonce          [NonceSize]byte
	PropertyID     [PropertyIDSize]byte
	Expiration     time.Time
	UserData       []byte
}

func NewPuzzle() (*Puzzle, error) {
	p := &Puzzle{
		UserData:       make([]byte, UserDataSize),
		Expiration:     time.Now().Add(6 * time.Hour),
		Difficulty:     65,
		SolutionsCount: 16,
		Version:        1,
	}

	if _, err := io.ReadFull(rand.Reader, p.Nonce[:]); err != nil {
		slog.Error("Failed to read random nonce", common.ErrAttr(err))
		return nil, err
	}

	if _, err := io.ReadFull(rand.Reader, p.UserData); err != nil {
		slog.Error("Failed to read random user data", common.ErrAttr(err))
		return nil, err
	}

	return p, nil
}

func (p *Puzzle) Valid() bool {
	return (p.Version > 0) &&
		(p.Difficulty > 0) &&
		(p.SolutionsCount > 0) &&
		//(len(p.Nonce) == NonceSize) &&
		(!p.Expiration.IsZero()) &&
		(len(p.UserData) == UserDataSize)
}

func (p *Puzzle) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	binary.Write(&buf, binary.LittleEndian, p.Version)
	binary.Write(&buf, binary.LittleEndian, p.PropertyID)
	binary.Write(&buf, binary.LittleEndian, p.Nonce)
	binary.Write(&buf, binary.LittleEndian, p.Difficulty)
	binary.Write(&buf, binary.LittleEndian, p.SolutionsCount)
	binary.Write(&buf, binary.LittleEndian, uint32(p.Expiration.Unix()))
	binary.Write(&buf, binary.LittleEndian, p.UserData)

	return buf.Bytes(), nil
}

func (p *Puzzle) UnmarshalBinary(data []byte) error {
	if len(data) < (PropertyIDSize + NonceSize + UserDataSize + 7) {
		return io.ErrShortBuffer
	}

	var offset int

	p.Version = data[0]
	offset += 1

	copy(p.PropertyID[:], data[offset:offset+PropertyIDSize])
	offset += PropertyIDSize

	copy(p.Nonce[:], data[offset:offset+NonceSize])
	offset += NonceSize

	p.Difficulty = data[offset]
	offset += 1

	p.SolutionsCount = data[offset]
	offset += 1

	unixExpiration := int64(binary.LittleEndian.Uint32(data[offset : offset+4]))
	p.Expiration = time.Unix(unixExpiration, 0)
	offset += 4

	p.UserData = make([]byte, UserDataSize)
	copy(p.UserData, data[offset:offset+UserDataSize])

	return nil
}
