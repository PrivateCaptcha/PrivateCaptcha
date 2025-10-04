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

func (s *signature) BinarySize() int {
	return 3 + len(s.Hash)
}

func (s *signature) WriteTo(w io.Writer) (int64, error) {
	if err := binary.Write(w, binary.LittleEndian, s.Version); err != nil {
		return 0, err
	}
	if err := binary.Write(w, binary.LittleEndian, s.Flags); err != nil {
		return 1, err
	}
	if err := binary.Write(w, binary.LittleEndian, s.Fingerprint); err != nil {
		return 2, err
	}
	n, err := w.Write(s.Hash)
	return 3 + int64(n), err
}

func (s *signature) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *signature) UnmarshalBinary(data []byte) error {
	if len(data) < 3 {
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
