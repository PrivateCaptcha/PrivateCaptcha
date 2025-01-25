package puzzle

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	randv2 "math/rand/v2"
	"strconv"
	"time"
)

const (
	PropertyIDSize = 16
	UserDataSize   = 16
	ValidityPeriod = 6 * time.Hour
	puzzleVersion  = 1
	solutionsCount = 16
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

func NewPuzzle() *Puzzle {
	return &Puzzle{
		Version:        puzzleVersion,
		Difficulty:     0,
		SolutionsCount: solutionsCount,
		PropertyID:     [16]byte{},
		PuzzleID:       0,
		UserData:       make([]byte, UserDataSize),
		Expiration:     time.Time{},
	}
}

func (p *Puzzle) Init(propertyID [16]byte, difficulty uint8) error {
	const maxTries = 10
	var puzzleID uint64

	for i := 0; (i < maxTries) && (puzzleID == 0); i++ {
		puzzleID = randv2.Uint64()
	}

	p.PropertyID = propertyID
	p.PuzzleID = puzzleID
	p.Difficulty = difficulty
	p.Expiration = time.Now().UTC().Add(ValidityPeriod)

	if _, err := io.ReadFull(rand.Reader, p.UserData); err != nil {
		return err
	}

	return nil
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

	return nil
}
