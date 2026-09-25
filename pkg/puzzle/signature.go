package puzzle

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
)

const (
	signatureVersion       = 1
	flagWithExtra    uint8 = 1 << 1
	signatureSize          = 3 + sha1.Size
)

var (
	errInvalidSignatureVersion = errors.New("invalid signature version")
	errInvalidSignatureFlags   = errors.New("invalid signature flags")
	errInvalidSignatureLength  = errors.New("invalid signature length")
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
	if len(data) < signatureSize {
		return io.ErrShortBuffer
	}
	if len(data) > signatureSize {
		return errInvalidSignatureLength
	}
	if data[0] != signatureVersion {
		return errInvalidSignatureVersion
	}
	if data[1]&^flagWithExtra != 0 {
		return errInvalidSignatureFlags
	}

	s.Version = data[0]
	s.Flags = data[1]
	s.Fingerprint = data[2]
	s.Hash = data[3:]
	return nil
}
