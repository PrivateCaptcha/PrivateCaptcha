package puzzle

import (
	"bytes"
	"encoding/binary"
	"io"
)

const (
	signatureVersion       = 1
	flagWithExtra    uint8 = 1 << iota
)

type signature struct {
	Version     uint8
	Fingerprint uint8
	Flags       uint8
	Hash        []byte
}

func newSignature(hash []byte, salt *Salt, extraSalt []byte) *signature {
	var flags uint8 = 0

	if len(extraSalt) > 0 {
		flags |= flagWithExtra
	}

	return &signature{
		Version:     signatureVersion,
		Fingerprint: salt.Fingerprint(),
		Flags:       flags,
		Hash:        hash,
	}
}

func (s *signature) HasExtra() bool {
	return s.Flags&flagWithExtra != 0
}

func (s *signature) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	binary.Write(&buf, binary.LittleEndian, s.Version)
	binary.Write(&buf, binary.LittleEndian, s.Flags)
	binary.Write(&buf, binary.LittleEndian, s.Fingerprint)
	binary.Write(&buf, binary.LittleEndian, s.Hash)

	return buf.Bytes(), nil
}

func (s *signature) UnmarshalBinary(data []byte) error {
	if len(data) < 2 {
		return io.ErrShortBuffer
	}

	var offset int

	s.Version = data[0]
	offset += 1

	s.Flags = data[offset]
	offset += 1

	s.Fingerprint = data[offset]
	offset += 1

	s.Hash = data[offset:]
	return nil
}
